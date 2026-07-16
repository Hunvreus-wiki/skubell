// Package protect builds bulk page-protection change plans: given selected titles and the user's target protection
// settings, it reads current protection, picks the restriction types applicable to each page (existing pages take
// edit/move, missing titles take create — each narrowed to the types the wiki actually offers), preserves any current
// protection the UI doesn't manage (e.g. upload on File pages), tracks cascade, computes the resulting protection,
// drops no-ops, and emits one OpProtectPage per actually-changing page. See local-notes/phase2-protection.md.
package protect

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

// Restriction types applicable to a page by its existence. upload is files-only and deferred.
var (
	existingPageTypes = []string{"edit", "move"}
	missingPageTypes  = []string{"create"}
)

// permanent is the canonical form of every "no expiry" spelling, used for comparison and display.
const permanent = "infinity"

// TypeProtection is the current protection of one restriction type on a page.
type TypeProtection struct {
	Level   string // "" = no restriction; else a wiki level (autoconfirmed, sysop, …)
	Expiry  string // "infinity" or a timestamp; "" when unprotected
	Cascade bool
}

// PageProtection is a page's current protection plus whether it exists.
type PageProtection struct {
	Title       string
	Exists      bool
	Protections map[string]TypeProtection // restriction type -> current protection (only protected types present)
}

// Reader reads current protection + existence for a batch of titles. It takes a context so the read phase can be
// cancelled (it runs off the UI goroutine).
type Reader interface {
	PageProtections(ctx context.Context, titles []string) (map[string]PageProtection, error)
}

// TypeSetting is the user's target for one restriction type. Keep* preserve the page's current value (used e.g. to make
// a temporary protection permanent without knowing/retyping its level: KeepLevel + Expiry="infinite").
type TypeSetting struct {
	KeepLevel  bool
	Level      string // target level when !KeepLevel; "" or "all" = remove protection
	KeepExpiry bool
	Expiry     string // target expiry when !KeepExpiry; "" defaults to permanent
}

// Settings is the target protection the user configured in the Options step.
type Settings struct {
	ByType  map[string]TypeSetting // restriction type -> target
	Cascade bool
	Reason  string
}

// TypeChange is one restriction type's before→after, for the verification display.
type TypeChange struct {
	Type       string
	FromLevel  string // "" shown by the UI as "(none)"
	FromExpiry string
	ToLevel    string
	ToExpiry   string
}

// PlanItem is one page's protection change (or non-change).
type PlanItem struct {
	Title       string
	Exists      bool
	Changes     []TypeChange  // per applicable type
	Changed     bool          // true when at least one applicable type (or cascade) changes
	FromCascade bool          // current direct cascade state
	ToCascade   bool          // resulting cascade state
	Invalid     bool          // true when the request can't be made (e.g. cascade with a non-cascading level)
	Note        string        // why it is invalid, when Invalid
	Op          ops.Operation // the OpProtectPage to run; zero when unchanged or invalid
}

// Plan is the outcome of BuildPlan: display-ordered items and counts.
type Plan struct {
	Items     []PlanItem
	Change    int
	Unchanged int
	Invalid   int
}

// BuildPlan computes the protection plan. cascadingLevels are the wiki's levels that permit cascade (siteinfo
// restrictions.cascadinglevels). restrictionTypes are the types the wiki offers (siteinfo restrictions.types); a page's
// managed types are narrowed to these so the plan never names a type the wiki would reject. Empty restrictionTypes
// disables that filtering (unknown wiki → fall back to the built-in defaults).
func BuildPlan(
	ctx context.Context, reader Reader, titles []string, settings Settings, cascadingLevels, restrictionTypes []string,
) (Plan, error) {
	current, err := reader.PageProtections(ctx, titles)
	if err != nil {
		return Plan{}, fmt.Errorf("read current protection: %w", err)
	}

	sorted := slices.Clone(titles)
	sort.Strings(sorted)

	plan := Plan{}
	seen := map[string]struct{}{}
	for _, title := range sorted {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		page, ok := current[title]
		// Dedup by the API-normalized title so aliases of one page (Foo_bar / Foo bar) emit a single write; fall back
		// to the input spelling when the read didn't return the page (e.g. an invalid title).
		canonical := title
		if ok && strings.TrimSpace(page.Title) != "" {
			canonical = page.Title
		} else {
			page.Title = title
		}
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}

		item := buildItem(page, settings, cascadingLevels, restrictionTypes)
		plan.Items = append(plan.Items, item)
		switch {
		case item.Invalid:
			plan.Invalid++
		case item.Changed:
			plan.Change++
		default:
			plan.Unchanged++
		}
	}
	return plan, nil
}

// applicableTypes are the restriction types the plan manages for a page by its existence, narrowed to the types the
// wiki offers (supported). Naming a type the wiki doesn't support makes action=protect reject the whole request, so it
// must never be planned. Empty supported means the wiki is unknown and falls back to the unfiltered defaults.
func applicableTypes(exists bool, supported []string) []string {
	base := existingPageTypes
	if !exists {
		base = missingPageTypes
	}
	if len(supported) == 0 {
		return base
	}
	out := make([]string, 0, len(base))
	for _, typ := range base {
		if slices.Contains(supported, typ) {
			out = append(out, typ)
		}
	}
	return out
}

func buildItem(page PageProtection, s Settings, cascadingLevels, restrictionTypes []string) PlanItem {
	item := PlanItem{Title: page.Title, Exists: page.Exists}
	params := map[string]string{"title": page.Title}

	managed := applicableTypes(page.Exists, restrictionTypes)
	for _, typ := range managed {
		cur := page.Protections[typ]
		set, ok := s.ByType[typ]
		if !ok {
			// Absent type → preserve current; never silently remove protection.
			set = TypeSetting{KeepLevel: true, KeepExpiry: true}
		}
		toLevel, toExpiry := resolveTarget(cur, set)

		item.Changes = append(item.Changes, TypeChange{
			Type: typ, FromLevel: cur.Level, FromExpiry: displayExpiry(cur.Level, cur.Expiry),
			ToLevel: normLevel(toLevel), ToExpiry: displayExpiry(toLevel, toExpiry),
		})
		if !sameProtection(cur.Level, cur.Expiry, toLevel, toExpiry) {
			item.Changed = true
		}

		// Send every applicable type explicitly: action=protect replaces the whole set, so an omitted type would be
		// unprotected. "" level → the translator emits "all" (no restriction).
		params["protect_"+typ] = normLevel(toLevel)
		params["expiry_"+typ] = defaultPermanentExpiry(toExpiry)
	}

	// Carry over current protection the UI doesn't manage (e.g. upload on File pages): action=protect replaces the
	// whole set, so a type left out of the request would be silently unprotected. Preserved unchanged, so not a change.
	for typ, cur := range page.Protections {
		if slices.Contains(managed, typ) || isNoRestriction(cur.Level) {
			continue
		}
		params["protect_"+typ] = normLevel(cur.Level)
		params["expiry_"+typ] = defaultPermanentExpiry(cur.Expiry)
	}

	// Cascade is a property of an existing page's edit restriction: MediaWiki stores it only there, and discards it for
	// a missing title or when edit is left unprotected. Gate ToCascade on both so the preview and the request match
	// reality (a move-only or create change never cascades), and validate only the edit level for cascade eligibility —
	// a non-cascading move/create/upload level is irrelevant and must not reject an otherwise valid change.
	editLevel := normLevel(params["protect_edit"])
	cascadeApplies := s.Cascade && page.Exists && editLevel != ""
	if cascadeApplies && !slices.Contains(cascadingLevels, editLevel) {
		item.Invalid = true
		item.Note = fmt.Sprintf("cascade requires a cascading edit level; %q is not one", editLevel)
		return item
	}
	// FromCascade is the page's own (direct) cascade — sourced/inherited entries are excluded upstream so they can't
	// masquerade as direct state. A change to cascade alone (level/expiry unchanged) is still a change to apply.
	item.FromCascade = pageCascades(page)
	item.ToCascade = cascadeApplies
	if item.FromCascade != item.ToCascade {
		item.Changed = true
	}
	if item.ToCascade {
		params["cascade"] = "true"
	}

	if reason := strings.TrimSpace(s.Reason); reason != "" {
		params["reason"] = reason
	}

	if item.Changed {
		item.Op = ops.Operation{Type: ops.OpProtectPage, Params: params, Description: describe(page)}
	}
	return item
}

// pageCascades reports whether the page carries its own (direct) cascade protection on any type.
func pageCascades(page PageProtection) bool {
	for _, p := range page.Protections {
		if p.Cascade {
			return true
		}
	}
	return false
}

// resolveTarget applies a TypeSetting over the current protection, honoring the Keep* flags.
func resolveTarget(cur TypeProtection, set TypeSetting) (level, expiry string) {
	level = cur.Level
	if !set.KeepLevel {
		level = strings.TrimSpace(set.Level)
	}
	expiry = cur.Expiry
	if !set.KeepExpiry {
		expiry = strings.TrimSpace(set.Expiry)
	}
	return level, expiry
}

// normLevel canonicalizes a level: "all" (the API's "no restriction") becomes "" so display/compare use one spelling.
func normLevel(level string) string {
	if strings.EqualFold(strings.TrimSpace(level), "all") {
		return ""
	}
	return strings.TrimSpace(level)
}

func isNoRestriction(level string) bool { return normLevel(level) == "" }

// normExpiry folds every "permanent" spelling to one value; other expiries are compared verbatim.
func normExpiry(expiry string) string {
	switch strings.ToLower(strings.TrimSpace(expiry)) {
	case "", "infinite", "infinity", "indefinite", "never":
		return permanent
	default:
		return strings.TrimSpace(expiry)
	}
}

// sameProtection reports whether two (level, expiry) states are equivalent. Expiry is irrelevant when unprotected.
func sameProtection(aLevel, aExpiry, bLevel, bExpiry string) bool {
	if normLevel(aLevel) != normLevel(bLevel) {
		return false
	}
	if isNoRestriction(aLevel) {
		return true
	}
	return normExpiry(aExpiry) == normExpiry(bExpiry)
}

// displayExpiry is the expiry shown for a (level, expiry): blank when unprotected, else the canonical/verbatim value.
func displayExpiry(level, expiry string) string {
	if isNoRestriction(level) {
		return ""
	}
	return normExpiry(expiry)
}

// defaultPermanentExpiry maps a blank target expiry to the permanent sentinel for the API call.
func defaultPermanentExpiry(expiry string) string {
	if strings.TrimSpace(expiry) == "" {
		return "infinite"
	}
	return strings.TrimSpace(expiry)
}

func describe(page PageProtection) string {
	return fmt.Sprintf("Change protection of %q", page.Title)
}
