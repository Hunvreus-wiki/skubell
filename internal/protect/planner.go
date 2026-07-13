// Package protect builds bulk page-protection change plans: given selected titles and the user's target protection
// settings, it reads current protection, filters the restriction types applicable to each page (existing pages take
// edit/move, missing titles take create), computes the resulting protection, drops no-ops, and emits one OpProtectPage
// per actually-changing page. See local-notes/phase2-protection.md.
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
	Title   string
	Exists  bool
	Changes []TypeChange  // per applicable type
	Changed bool          // true when at least one applicable type changes
	Invalid bool          // true when the request can't be made (e.g. cascade with a non-cascading level)
	Note    string        // why it is invalid, when Invalid
	Op      ops.Operation // the OpProtectPage to run; zero when unchanged or invalid
}

// Plan is the outcome of BuildPlan: display-ordered items and counts.
type Plan struct {
	Items     []PlanItem
	Change    int
	Unchanged int
	Invalid   int
}

// BuildPlan computes the protection plan. cascadingLevels are the wiki's levels that permit cascade (siteinfo
// restrictions.cascadinglevels).
func BuildPlan(
	ctx context.Context, reader Reader, titles []string, settings Settings, cascadingLevels []string,
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
		if _, dup := seen[title]; dup {
			continue
		}
		seen[title] = struct{}{}

		page := current[title]
		page.Title = title
		item := buildItem(page, settings, cascadingLevels)
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

func applicableTypes(exists bool) []string {
	if exists {
		return existingPageTypes
	}
	return missingPageTypes
}

func buildItem(page PageProtection, s Settings, cascadingLevels []string) PlanItem {
	item := PlanItem{Title: page.Title, Exists: page.Exists}
	params := map[string]string{"title": page.Title}
	anyRealProtection := false

	for _, typ := range applicableTypes(page.Exists) {
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
		if !isNoRestriction(toLevel) {
			anyRealProtection = true
		}
	}

	if s.Cascade {
		if bad := firstNonCascadingLevel(params, cascadingLevels); bad != "" {
			item.Invalid = true
			item.Note = fmt.Sprintf("cascade requires a cascading level; %q is not one", bad)
			return item
		}
		if anyRealProtection {
			params["cascade"] = "true"
		}
	}
	if reason := strings.TrimSpace(s.Reason); reason != "" {
		params["reason"] = reason
	}

	if item.Changed {
		item.Op = ops.Operation{Type: ops.OpProtectPage, Params: params, Description: describe(page)}
	}
	return item
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

func firstNonCascadingLevel(params map[string]string, cascadingLevels []string) string {
	for _, typ := range []string{"edit", "create", "move", "upload"} {
		level := normLevel(params["protect_"+typ])
		if level == "" {
			continue
		}
		if !slices.Contains(cascadingLevels, level) {
			return level
		}
	}
	return ""
}

func describe(page PageProtection) string {
	return fmt.Sprintf("Change protection of %q", page.Title)
}
