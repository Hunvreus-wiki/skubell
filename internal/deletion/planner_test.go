package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/ops"
)

func itemTitles(plan Plan) []string {
	titles := make([]string, 0, len(plan.Items))
	for _, item := range plan.Items {
		titles = append(titles, item.Title)
	}
	return titles
}

func findItem(t *testing.T, plan Plan, title string) PlanItem {
	t.Helper()
	for _, item := range plan.Items {
		if item.Title == title {
			return item
		}
	}
	t.Fatalf("item %q not found in %v", title, itemTitles(plan))
	return PlanItem{}
}

func TestBuildPlanBasicDeletes(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(nil, []string{"Apple", "Banana", "Carrot"}, PlanOptions{Reason: "Cleanup"})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Banana", "Carrot"}, itemTitles(plan))
	require.Equal(t, 3, plan.OperationCount())
	require.Equal(t, 3, plan.PageCount)

	apple := findItem(t, plan, "Apple")
	require.Equal(t, ops.OpDeletePage, apple.Operation.Type)
	require.Equal(t, "Cleanup", apple.Operation.Params["reason"])
	require.False(t, apple.Derived)
	require.False(t, apple.HasTalkPage)
	require.NotContains(t, apple.Operation.Params, paramDeleteTalk)
}

func TestBuildPlanTalkOptionUsesDeletetalkWhenTalkExists(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Talk:Apple":  "Apple",
			"Talk:Banana": "Banana",
		},
		TalkPages: map[string]string{
			"Apple":  "Talk:Apple",
			"Banana": "Talk:Banana",
		},
		ExistingPages: map[string]struct{}{
			"Talk:Apple": {}, // Talk:Banana intentionally absent → does not exist
		},
	}

	plan, err := BuildPlan(provider, []string{"Apple", "Banana"}, PlanOptions{IncludeTalk: true})
	require.NoError(t, err)
	require.Equal(t, 2, plan.OperationCount())
	require.Equal(t, 3, plan.PageCount) // Apple + Talk:Apple + Banana (Talk:Banana absent)

	apple := findItem(t, plan, "Apple")
	require.True(t, apple.HasTalkPage)
	require.Equal(t, "true", apple.Operation.Params[paramDeleteTalk])

	banana := findItem(t, plan, "Banana")
	require.False(t, banana.HasTalkPage)
	require.NotContains(t, banana.Operation.Params, paramDeleteTalk)
}

func TestBuildPlanRedirectsAreTransitive(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		Redirects: map[string][]string{
			"Apple":     {"Cider"},
			"Cider":     {"Old-Apple"}, // transitive: Old-Apple -> Cider -> Apple
			"Old-Apple": nil,
		},
	}

	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{IncludeRedirect: true})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Cider", "Old-Apple"}, itemTitles(plan))

	old := findItem(t, plan, "Old-Apple")
	require.True(t, old.Derived)
	require.Equal(t, "Apple", old.Root) // ultimate root, not the immediate parent
	require.Equal(t, "Cider", old.Operation.Params[paramRedirectTarget])
}

func TestBuildPlanRedirectCycleTerminates(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		Redirects: map[string][]string{
			"Apple": {"Bravo"},
			"Bravo": {"Apple"}, // cycle
		},
	}

	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{IncludeRedirect: true})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Bravo"}, itemTitles(plan))
}

func TestBuildPlanReportsProgress(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		Redirects: map[string][]string{
			"Apple":     {"Cider"},
			"Cider":     {"Old-Apple"}, // Apple -> Cider -> Old-Apple: three pages, handled one per iteration
			"Old-Apple": nil,
		},
	}

	var processedSeq, foundSeq []int
	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{
		IncludeRedirect: true,
		OnProgress: func(processed, found int) {
			processedSeq = append(processedSeq, processed)
			foundSeq = append(foundSeq, found)
		},
	})
	require.NoError(t, err)
	require.Len(t, plan.Items, 3)

	require.Equal(t, []int{1, 2, 3}, processedSeq) // one callback per page, processed increments monotonically
	require.Equal(t, 3, foundSeq[len(foundSeq)-1]) // ends with every page discovered
	for i := range processedSeq {
		require.GreaterOrEqual(t, foundSeq[i], processedSeq[i]) // discovery never lags behind processing
	}
}

func TestPlanRemoveMainEntryDropsWholeGroup(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		Redirects: map[string][]string{
			"Apple": {"Cider"}, "Cider": {"Old-Apple"}, "Old-Apple": nil, // Old-Apple -> Cider -> Apple
			"Banana": nil,
		},
	}
	plan, err := BuildPlan(provider, []string{"Apple", "Banana"}, PlanOptions{IncludeRedirect: true})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Cider", "Old-Apple", "Banana"}, itemTitles(plan))

	got := plan.RemoveWithDependents("Apple") // main entry: drops its whole redirect group, keeps Banana
	require.Equal(t, []string{"Banana"}, itemTitles(got))
	require.Equal(t, 1, got.PageCount)
	require.Equal(t, []string{"Apple", "Cider", "Old-Apple", "Banana"}, itemTitles(plan)) // original untouched
}

func TestPlanRemoveDerivedEntryDropsOnlyItsSubtree(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		Redirects: map[string][]string{
			"Apple": {"Cider"}, "Cider": {"Old-Apple"}, "Old-Apple": nil,
		},
	}
	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{IncludeRedirect: true})
	require.NoError(t, err)

	// Cider is derived: dropping it also drops Old-Apple (which redirects to Cider), but keeps the selected Apple.
	require.Equal(t, []string{"Apple"}, itemTitles(plan.RemoveWithDependents("Cider")))
	// The leaf Old-Apple drops only itself.
	require.Equal(t, []string{"Apple", "Cider"}, itemTitles(plan.RemoveWithDependents("Old-Apple")))
}

func TestPlanRemoveRecomputesTalkPageCount(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages:  map[string]string{"Talk:Apple": "Apple", "Talk:Banana": "Banana"},
		TalkPages:     map[string]string{"Apple": "Talk:Apple", "Banana": "Talk:Banana"},
		ExistingPages: map[string]struct{}{"Talk:Apple": {}, "Talk:Banana": {}},
	}
	plan, err := BuildPlan(provider, []string{"Apple", "Banana"}, PlanOptions{IncludeTalk: true})
	require.NoError(t, err)
	require.Equal(t, 4, plan.PageCount) // Apple+Talk:Apple, Banana+Talk:Banana

	got := plan.RemoveWithDependents("Apple")
	require.Equal(t, []string{"Banana"}, itemTitles(got))
	require.Equal(t, 2, got.PageCount) // Banana + its riding-along talk page
}

func TestPlanRemoveUnknownTitleIsNoop(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(nil, []string{"Apple", "Banana"}, PlanOptions{})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Banana"}, itemTitles(plan.RemoveWithDependents("Nope")))
}

// A talk page that is itself a redirect, whose subject page is also being deleted, must NOT get a standalone delete —
// the subject's deletetalk removes it. This is the case that a single-pass planner double-deletes.
func TestBuildPlanTalkRedirectCoveredByDeletetalk(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Talk:Apple":     "Apple",
			"Talk:Old-Apple": "Old-Apple",
		},
		TalkPages: map[string]string{
			"Apple":     "Talk:Apple",
			"Old-Apple": "Talk:Old-Apple",
		},
		ExistingPages: map[string]struct{}{
			"Talk:Apple": {}, "Talk:Old-Apple": {},
		},
		Redirects: map[string][]string{
			"Apple":      {"Old-Apple"},      // Old-Apple redirects to Apple
			"Talk:Apple": {"Talk:Old-Apple"}, // Talk:Old-Apple redirects to Talk:Apple
		},
	}

	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{IncludeTalk: true, IncludeRedirect: true})
	require.NoError(t, err)

	// Two operations (Apple, Old-Apple), each with deletetalk; four pages total.
	require.Equal(t, []string{"Apple", "Old-Apple"}, itemTitles(plan))
	require.Equal(t, 4, plan.PageCount)
	require.Equal(t, "true", findItem(t, plan, "Apple").Operation.Params[paramDeleteTalk])
	require.Equal(t, "true", findItem(t, plan, "Old-Apple").Operation.Params[paramDeleteTalk])
}

// A talk-page redirect whose subject page is NOT being deleted is an orphan and gets its own standalone delete (no
// deletetalk).
func TestBuildPlanOrphanTalkRedirect(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Talk:Apple": "Apple",
			"Talk:Pomme": "Pomme", // Pomme is not in the deletion set
		},
		TalkPages: map[string]string{
			"Apple": "Talk:Apple",
		},
		ExistingPages: map[string]struct{}{"Talk:Apple": {}},
		Redirects: map[string][]string{
			"Talk:Apple": {"Talk:Pomme"}, // Talk:Pomme redirects to Talk:Apple
		},
	}

	plan, err := BuildPlan(provider, []string{"Apple"}, PlanOptions{IncludeTalk: true, IncludeRedirect: true})
	require.NoError(t, err)
	require.Equal(t, []string{"Apple", "Talk:Pomme"}, itemTitles(plan))
	require.Equal(t, 3, plan.PageCount) // Apple + Talk:Apple + Talk:Pomme

	pomme := findItem(t, plan, "Talk:Pomme")
	require.True(t, pomme.TalkPage)
	require.True(t, pomme.Derived)
	require.Equal(t, "Apple", pomme.Root)
	require.Equal(t, "Pomme", pomme.SubjectTitle) // sorts at its subject, not clustered
	require.NotContains(t, pomme.Operation.Params, paramDeleteTalk)
}

// Talk pages are resolved by namespace, not a "Talk:" prefix — Category talk, User talk, etc. all pair correctly.
func TestBuildPlanCrossNamespaceTalkPairs(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Category talk:Fruit":   "Category:Fruit",
			"Category talk:Produce": "Category:Produce",
		},
		TalkPages: map[string]string{
			"Category:Fruit":   "Category talk:Fruit",
			"Category:Produce": "Category talk:Produce",
		},
		ExistingPages: map[string]struct{}{
			"Category talk:Fruit": {}, // Category talk:Produce absent → no 💬 for Produce
		},
		Redirects: map[string][]string{
			"Category:Fruit": {"Category:Produce"},
		},
	}

	plan, err := BuildPlan(
		provider,
		[]string{"Category:Fruit"},
		PlanOptions{IncludeTalk: true, IncludeRedirect: true},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"Category:Fruit", "Category:Produce"}, itemTitles(plan))
	require.True(t, findItem(t, plan, "Category:Fruit").HasTalkPage)
	require.False(t, findItem(t, plan, "Category:Produce").HasTalkPage)
	require.Equal(t, 3, plan.PageCount) // Category:Fruit + its talk + Category:Produce
}

func TestBuildPlanSortsTalkAfterMainGroupedUnderRoot(t *testing.T) {
	t.Parallel()

	provider := &ops.MockDataProvider{
		SubjectPages: map[string]string{
			"Talk:Apple":     "Apple",
			"Talk:Cider":     "Cider",
			"Talk:Old-Apple": "Old-Apple",
			"Talk:Banana":    "Banana",
			"Talk:Berry":     "Berry", // orphan (Berry not deleted)
		},
		TalkPages: map[string]string{
			"Apple":     "Talk:Apple",
			"Cider":     "Talk:Cider",
			"Old-Apple": "Talk:Old-Apple",
			"Banana":    "Talk:Banana",
		},
		ExistingPages: map[string]struct{}{
			"Talk:Apple": {}, "Talk:Cider": {}, "Talk:Old-Apple": {}, "Talk:Banana": {},
		},
		Redirects: map[string][]string{
			"Apple":      {"Cider"},
			"Cider":      {"Old-Apple"},
			"Talk:Apple": {"Talk:Berry"},
		},
	}

	plan, err := BuildPlan(
		provider,
		[]string{"Banana", "Apple"},
		PlanOptions{IncludeTalk: true, IncludeRedirect: true},
	)
	require.NoError(t, err)

	// Root "Apple" block first (alphabetical), root pinned, derived by subject title (Berry, Cider, Old-Apple); then
	// root "Banana".
	require.Equal(t, []string{
		"Apple",
		"Talk:Berry",
		"Cider",
		"Old-Apple",
		"Banana",
	}, itemTitles(plan))
}

// TestBuildPlanBatchesEachLevelOnce pins the shape of the walk: one request per level of the redirect chain, not one
// per page. Asking page by page turned a page list into a round trip per page, and the walk is transitive, so it
// compounded — while the bar sat still on whichever page owned the chain.
func TestBuildPlanBatchesEachLevelOnce(t *testing.T) {
	t.Parallel()

	var batches [][]string
	provider := &recordingRedirectProvider{
		MockDataProvider: ops.MockDataProvider{Redirects: map[string][]string{
			"Apple":  {"Cider"},     // level 2
			"Cider":  {"Old-Apple"}, // level 3
			"Banana": {"Plantain"},  // level 2, alongside Cider
		}},
		seen: &batches,
	}

	plan, err := BuildPlan(provider, []string{"Apple", "Banana"}, PlanOptions{IncludeRedirect: true})
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"Apple", "Cider", "Old-Apple", "Banana", "Plantain"}, itemTitles(plan))

	// One request per level, each carrying that level's whole set — three levels deep, three requests.
	require.Equal(t, [][]string{
		{"Apple", "Banana"},
		{"Cider", "Plantain"},
		{"Old-Apple"},
	}, batches)
}

type recordingRedirectProvider struct {
	ops.MockDataProvider
	seen *[][]string
}

func (r *recordingRedirectProvider) GetRedirects(titles []string) (map[string][]string, error) {
	*r.seen = append(*r.seen, append([]string(nil), titles...))
	return r.MockDataProvider.GetRedirects(titles)
}
