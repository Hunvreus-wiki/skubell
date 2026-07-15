package deletion

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

const (
	paramDeleteTalk     = "delete_talk"
	paramRedirectTarget = "redirect_target"
)

// PlanOptions controls deletion plan behavior.
type PlanOptions struct {
	Reason          string
	IncludeTalk     bool
	IncludeRedirect bool
	// OnProgress, when set, reports discovery as it runs: how many reference pages are fully processed, how many there
	// are, and how many pages have been found.
	//
	// A reference page — one of the caller's own list, deduplicated — is processed once everything depending on it has
	// surfaced, so a page whose redirects lead to further redirects stays unprocessed until the last of them is found.
	// The reference list is the total because it is the only one known before discovery starts and the only one that
	// cannot grow while the caller is reading it.
	//
	// A page is found once it is identified for deletion, so redirects and existing talk pages count and found normally
	// runs ahead of processed, ending at the plan's PageCount. A talk page is only found once the wiki confirms it
	// exists, which is why the last call comes after discovery has finished.
	//
	// It runs on BuildPlan's own goroutine; the caller is responsible for marshaling any UI work.
	OnProgress func(processed, total, found int)
	// OnTalkCheck, when set, reports the associated-talk-page existence check — the one question discovery cannot
	// answer from a title alone. It is asked in batches rather than page by page, so it reports the titles resolved of
	// those to resolve and moves in jumps. Same goroutine and caveat as OnProgress.
	OnTalkCheck func(done, total int)
}

// PlanItem is one deletion row plus the metadata the UI needs to group, sort, and annotate it. A subject page and its
// associated talk page form a single item — HasTalkPage marks that the talk page rides along on the same deletetalk
// call — so only standalone (orphan) talk pages get their own item.
type PlanItem struct {
	Operation    ops.Operation
	Title        string // page title (== Operation.Params["title"])
	Root         string // ultimate selected page this item traces back to
	Derived      bool   // false for user-selected pages, true for discovered ones
	TalkPage     bool   // the item's own page is a talk page (standalone/orphan)
	HasTalkPage  bool   // subject page whose associated talk page exists (💬)
	SubjectTitle string // subject-namespace title, used as the display sort key
}

// Plan is the outcome of BuildPlan: display-ordered items plus a page count.
type Plan struct {
	Items     []PlanItem
	PageCount int // pages actually removed (operations + existing associated talk pages)
}

// OperationCount is the number of delete API calls the plan will make.
func (p Plan) OperationCount() int { return len(p.Items) }

// ExecutionPlan projects the plan into the executor's ExecutionPlan shape.
func (p Plan) ExecutionPlan() ops.ExecutionPlan {
	operations := make([]ops.Operation, 0, len(p.Items))
	for _, item := range p.Items {
		operations = append(operations, item.Operation)
	}
	return ops.ExecutionPlan{Module: "deletion", Operations: operations, ReadPhase: -1}
}

// RemoveWithDependents returns a copy of the plan without title and everything that depends on it, with PageCount
// recomputed and display order preserved. Removing a selected (non-derived) page drops its whole group — every
// discovered redirect and talk page tracing back to it. Removing a derived page drops only that page and the redirects
// that point at it (transitively), leaving the rest of its group intact. An unknown title is a no-op.
func (p Plan) RemoveWithDependents(title string) Plan {
	target, ok := p.itemByTitle(title)
	if !ok {
		return p
	}
	remove := p.dependents(target)
	remove[target.Title] = struct{}{}
	kept := make([]PlanItem, 0, len(p.Items))
	for _, item := range p.Items {
		if _, drop := remove[item.Title]; !drop {
			kept = append(kept, item)
		}
	}
	return Plan{Items: kept, PageCount: pageCountOf(kept)}
}

func (p Plan) itemByTitle(title string) (PlanItem, bool) {
	for _, item := range p.Items {
		if item.Title == title {
			return item, true
		}
	}
	return PlanItem{}, false
}

// dependents returns the titles that must go when target goes, excluding target itself.
func (p Plan) dependents(target PlanItem) map[string]struct{} {
	remove := map[string]struct{}{}
	if !target.Derived {
		// Selected page: its whole root group (every discovered redirect/talk page tracing back to it).
		for _, item := range p.Items {
			if item.Title != target.Title && item.Root == target.Root {
				remove[item.Title] = struct{}{}
			}
		}
		return remove
	}
	// Derived page: the redirects that point at it, transitively (redirect_target records what a page redirects to).
	children := map[string][]string{}
	for _, item := range p.Items {
		if parent := item.Operation.Params[paramRedirectTarget]; parent != "" {
			children[parent] = append(children[parent], item.Title)
		}
	}
	queue := append([]string(nil), children[target.Title]...)
	for len(queue) > 0 {
		title := queue[0]
		queue = queue[1:]
		if _, seen := remove[title]; seen {
			continue
		}
		remove[title] = struct{}{}
		queue = append(queue, children[title]...)
	}
	return remove
}

// pageCountOf counts the pages a set of items removes: one per item, plus one for each item's riding-along talk page.
func pageCountOf(items []PlanItem) int {
	count := 0
	for _, item := range items {
		count++
		if item.HasTalkPage {
			count++
		}
	}
	return count
}

// BuildPlan computes the full set of pages a deletion will remove: the selected pages, their associated talk pages
// (deleted via deletetalk on the same call), and — when redirects are included — the transitive closure of redirects
// that point at any of those pages (and at their talk pages). It returns display-ordered items and an accurate page
// count. Talk pages are resolved by namespace, not by a "Talk:" prefix, so every subject/talk namespace pair is handled
// (Category talk, User talk, …).
func BuildPlan(provider ops.DataProvider, titles []string, options PlanOptions) (Plan, error) {
	if provider == nil && (options.IncludeRedirect || options.IncludeTalk) {
		return Plan{}, errors.New("data provider is nil")
	}

	// Pass 1: breadth-first discovery of every page that will be deleted. root maps each page to the selected page it
	// ultimately traces back to; parent records the page it directly redirects to. The dedup in enqueue also breaks
	// redirect cycles.
	root := map[string]string{}
	parent := map[string]string{}
	var queue []string
	// pending counts the titles still queued for each reference page. A reference is processed only when it reaches
	// zero: that is the moment everything depending on that page has surfaced, redirects of redirects included.
	pending := map[string]int{}
	enqueue := func(title, redirectParent, ultimateRoot string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		if _, ok := root[title]; ok {
			return
		}
		root[title] = ultimateRoot
		if redirectParent != "" {
			parent[title] = redirectParent
		}
		pending[ultimateRoot]++
		queue = append(queue, title)
	}
	// A talk title is namespace arithmetic, not something the wiki reported: it is enqueued so that redirects pointing
	// at the talk page are found, but whether the page exists — and so whether it is a page identified for deletion at
	// all — is unknown until the existence check below. Counting those guesses is what made discovery announce twice
	// what would be deleted.
	unverified := map[string]struct{}{}

	// The reference pages are the caller's own list: the only total known before discovery starts, and so the only
	// honest denominator for progress. Everything else is found along the way.
	references := map[string]struct{}{}
	for _, title := range normalizeTitles(titles) {
		references[title] = struct{}{}
		enqueue(title, "", title)
	}

	subjectCache := map[string]string{}
	subjectOf := func(title string) (string, error) {
		if provider == nil {
			return "", nil // no provider (no options): every title is a subject page
		}
		if cached, ok := subjectCache[title]; ok {
			return cached, nil
		}
		subject, err := provider.GetSubjectPageTitle(title)
		if err != nil {
			return "", fmt.Errorf("resolve subject page of %q: %w", title, err)
		}
		subject = strings.TrimSpace(subject)
		subjectCache[title] = subject
		return subject, nil
	}

	// The walk goes a level at a time rather than a page at a time: the wiki answers a batch of titles in one request,
	// so asking page by page turned a list into a round trip per page — and since each redirect found is itself asked
	// about, that compounds down the chain.
	processed := 0
	for len(queue) > 0 {
		level := queue
		queue = nil

		redirectsOf := map[string][]string{}
		if options.IncludeRedirect {
			found, err := provider.GetRedirects(level)
			if err != nil {
				return Plan{}, fmt.Errorf("query redirects: %w", err)
			}
			redirectsOf = found
		}

		for _, title := range level {
			itemRoot := root[title]

			for _, redirect := range redirectsOf[title] {
				delete(unverified, redirect) // the wiki just named it, so it is a page and not a guess
				enqueue(redirect, title, itemRoot)
			}

			if options.IncludeTalk {
				subject, err := subjectOf(title)
				if err != nil {
					return Plan{}, err
				}
				if subject == "" {
					// title is a subject page: its talk page is removed by deletetalk. Enqueue it so redirects pointing
					// at the talk page are found too.
					talk, err := provider.GetTalkPageTitle(title)
					if err != nil {
						return Plan{}, fmt.Errorf("resolve talk page of %q: %w", title, err)
					}
					// "" means this namespace has no talk page at all, so there is nothing to guess at or enqueue.
					if talk = strings.TrimSpace(talk); talk != "" {
						if _, known := root[talk]; !known {
							unverified[talk] = struct{}{}
						}
						enqueue(talk, "", itemRoot)
					}
				}
			}

			// Decremented only now, after this page's own dependents have been enqueued, so a reference cannot look
			// finished while work it spawned is still queued.
			pending[itemRoot]--
			if pending[itemRoot] == 0 {
				processed++
			}

			if options.OnProgress != nil {
				options.OnProgress(processed, len(references), len(root)-len(unverified))
			}
		}
	}

	// Which associated talk pages actually exist — needed for the 💬 marker and an accurate page count. deletetalk is
	// only sent when the talk page exists.
	talkOf := map[string]string{}
	var talkTitles []string
	if options.IncludeTalk {
		for title := range root {
			subject, err := subjectOf(title)
			if err != nil {
				return Plan{}, err
			}
			if subject != "" {
				continue // title is a talk page, not a subject
			}
			talk, err := provider.GetTalkPageTitle(title)
			if err != nil {
				return Plan{}, fmt.Errorf("resolve talk page of %q: %w", title, err)
			}
			if talk = strings.TrimSpace(talk); talk == "" {
				continue
			}
			talkOf[title] = talk
			talkTitles = append(talkTitles, talk)
		}
	}
	existing := map[string]bool{}
	if len(talkTitles) > 0 {
		if options.OnTalkCheck != nil {
			options.OnTalkCheck(0, len(talkTitles))
		}
		found, err := provider.PagesExist(talkTitles)
		if err != nil {
			return Plan{}, fmt.Errorf("check associated talk page existence: %w", err)
		}
		existing = found
		if options.OnTalkCheck != nil {
			options.OnTalkCheck(len(talkTitles), len(talkTitles))
		}
		// This answers the one question discovery could not: a talk page that exists rides along on its subject's
		// deletetalk, so it is a page identified for deletion and joins the found count. The rest were never pages.
		for talk, exists := range existing {
			if exists {
				delete(unverified, talk)
			}
		}
		if options.OnProgress != nil {
			options.OnProgress(processed, len(references), len(root)-len(unverified))
		}
	}

	// Pass 2: assign one operation per row, plus display metadata.
	items := make([]PlanItem, 0, len(root))
	pageCount := 0
	for title, itemRoot := range root {
		subject, err := subjectOf(title)
		if err != nil {
			return Plan{}, err
		}
		derived := title != itemRoot

		if subject == "" {
			// Subject page (or a page whose namespace has no talk namespace).
			hasTalk := existing[talkOf[title]]
			item := PlanItem{
				Operation:    deleteOperation(title, options.Reason, hasTalk),
				Title:        title,
				Root:         itemRoot,
				Derived:      derived,
				HasTalkPage:  hasTalk,
				SubjectTitle: title,
			}
			if redirectParent := parent[title]; redirectParent != "" {
				item.Operation.Params[paramRedirectTarget] = redirectParent
			}
			items = append(items, item)
			pageCount++
			if hasTalk {
				pageCount++
			}
			continue
		}

		// Talk page. If its subject is also being deleted, the subject's deletetalk already removes it — no separate
		// operation, no double count.
		if _, ok := root[subject]; ok {
			continue
		}
		item := PlanItem{
			Operation:    deleteOperation(title, options.Reason, false),
			Title:        title,
			Root:         itemRoot,
			Derived:      derived,
			TalkPage:     true,
			SubjectTitle: subject,
		}
		if redirectParent := parent[title]; redirectParent != "" {
			item.Operation.Params[paramRedirectTarget] = redirectParent
		}
		items = append(items, item)
		pageCount++
	}

	sortPlanItems(items)
	return Plan{Items: items, PageCount: pageCount}, nil
}

// sortPlanItems orders rows for display: selected roots alphabetically; within a root's block the root first, then
// derived pages by subject title with a subject page immediately before its own talk page. This groups each talk page
// next to its main page instead of clustering all talk pages together.
func sortPlanItems(items []PlanItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Root != b.Root {
			return a.Root < b.Root
		}
		if a.Derived != b.Derived {
			return !a.Derived // the selected root is pinned first in its block
		}
		if a.SubjectTitle != b.SubjectTitle {
			return a.SubjectTitle < b.SubjectTitle
		}
		if a.TalkPage != b.TalkPage {
			return !a.TalkPage // subject page before its associated talk page
		}
		return a.Title < b.Title
	})
}

func deleteOperation(title, reason string, includeTalk bool) ops.Operation {
	params := map[string]string{
		"title": title,
	}
	if strings.TrimSpace(reason) != "" {
		params["reason"] = reason
	}
	description := fmt.Sprintf("Delete page %q", title)
	if includeTalk {
		params[paramDeleteTalk] = "true"
		description = fmt.Sprintf("Delete page %q and its talk page", title)
	}
	return ops.Operation{
		Type:        ops.OpDeletePage,
		Params:      params,
		Description: description,
	}
}

func normalizeTitles(titles []string) []string {
	out := make([]string, 0, len(titles))
	for _, title := range titles {
		trimmed := strings.TrimSpace(title)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
