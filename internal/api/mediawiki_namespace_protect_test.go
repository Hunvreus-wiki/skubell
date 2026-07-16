package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Protecting a MediaWiki-namespace page needs the same rights as deleting it, so a sysop lacking editsitecss/js is
// blocked on those config pages but fine on message/JSON pages.
func TestMissingProtectRightForSysop(t *testing.T) {
	t.Parallel()

	caps := sysopCaps() // editinterface + editsitejson, no editsitecss/js
	require.Empty(t, MissingProtectRight(caps, "MediaWiki:Sidebar"))
	require.Empty(t, MissingProtectRight(caps, "MediaWiki:Gadgets-definition.json"))
	require.Equal(t, RightEditSiteCSS, MissingProtectRight(caps, "MediaWiki:Common.css"))
	require.Equal(t, RightEditSiteJS, MissingProtectRight(caps, "MediaWiki:Common.js"))
	// Non-MediaWiki-namespace pages are not extra-gated for protection.
	require.Empty(t, MissingProtectRight(caps, "User:Toto/common.js"))
	require.Empty(t, MissingProtectRight(caps, "Apple"))
}

func TestProtectAccessMessageMapsEachRight(t *testing.T) {
	t.Parallel()

	caps := sysopCaps()
	require.Empty(t, ProtectAccessMessage(caps, "MediaWiki:Sidebar"))
	require.Equal(t, SiteCSSProtectGrantMessage(), ProtectAccessMessage(caps, "MediaWiki:Common.css"))
	require.Equal(t, SiteJSProtectGrantMessage(), ProtectAccessMessage(caps, "MediaWiki:Common.js"))

	noInterface := WikiCapabilities{UserRights: []string{"protect"}}
	require.Equal(t, MediaWikiNamespaceProtectGrantMessage(), ProtectAccessMessage(noInterface, "MediaWiki:Sidebar"))

	noJSON := WikiCapabilities{UserRights: []string{"protect", "editinterface"}}
	wantMsg := SiteJSONProtectGrantMessage()
	require.Equal(t, wantMsg, ProtectAccessMessage(noJSON, "MediaWiki:Gadgets-definition.json"))
}
