package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Hunvreus-wiki/skubell/internal/protect"
)

// TestProtectionLegendExplainsTheGlyphsTheRowsDraw ties the legend to the rows. The legend it replaced promised a ✎ for
// level and a ⏱ for expiry that no row has ever carried — the drift that comes of writing a glyph twice, once in the
// list and once in a translatable string, and expecting them to stay in step.
func TestProtectionLegendExplainsTheGlyphsTheRowsDraw(t *testing.T) {
	t.Parallel()

	unchanged := protectionRowText(protect.PlanItem{Title: "Banana"})
	invalid := protectionRowText(protect.PlanItem{Title: "Cherry", Invalid: true, InvalidLevel: "autoconfirmed"})

	require.True(t, strings.HasPrefix(unchanged, glyphUnchanged+" "), "got %q", unchanged)
	require.True(t, strings.HasPrefix(invalid, glyphWarning+" "), "got %q", invalid)
	require.Contains(t, invalid, `"autoconfirmed"`, "the invalid row names the blocking level")

	// A changing row carries no glyph at all: it shows the change itself, which is why the legend must not claim one.
	changing := protectionRowText(protect.PlanItem{Title: "Apple", Changed: true, Changes: []protect.TypeChange{
		{Type: "edit", ToLevel: "sysop", ToExpiry: "infinite"},
	}})
	require.NotContains(t, changing, glyphUnchanged)
	require.NotContains(t, changing, glyphWarning)
}
