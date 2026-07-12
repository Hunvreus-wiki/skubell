package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// sysopCaps mirrors a real sysop session observed on MediaWiki 1.43: it holds editinterface + editsitejson +
// edituserjson, but not editsitecss/editsitejs.
func sysopCaps() WikiCapabilities {
	return WikiCapabilities{
		UserRights: []string{"delete", "editinterface", "editsitejson", "edituserjson"},
	}
}

func TestRequiredDeleteRightsBySuffix(t *testing.T) {
	t.Parallel()

	caps := WikiCapabilities{}
	cases := []struct {
		title string
		want  []string
	}{
		{"MediaWiki:Sidebar", []string{RightEditInterface}},
		{"MediaWiki:Common.css", []string{RightEditInterface, RightEditSiteCSS}},
		{"MediaWiki:Common.js", []string{RightEditInterface, RightEditSiteJS}},
		{"MediaWiki:Gadgets-definition.json", []string{RightEditInterface, RightEditSiteJSON}},
		// Not the MediaWiki namespace: no extra gate on deletion.
		{"User:Toto/common.js", nil},
		{"User:Toto/common.css", nil},
		{"User talk:Toto/common.css", nil},
		{"Apple", nil},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, RequiredDeleteRights(caps, tc.title), tc.title)
	}
}

func TestMissingDeleteRightForSysop(t *testing.T) {
	t.Parallel()

	caps := sysopCaps()
	// Sysop has editinterface + editsitejson: message pages and JSON are fine.
	require.Empty(t, MissingDeleteRight(caps, "MediaWiki:Sidebar"))
	require.Empty(t, MissingDeleteRight(caps, "MediaWiki:Gadgets-definition.json"))
	// But sitewide CSS/JS require rights a sysop lacks.
	require.Equal(t, RightEditSiteCSS, MissingDeleteRight(caps, "MediaWiki:Common.css"))
	require.Equal(t, RightEditSiteJS, MissingDeleteRight(caps, "MediaWiki:Common.js"))
	// User-space and non-config pages are never gated on delete.
	require.Empty(t, MissingDeleteRight(caps, "User:Toto/common.js"))
	require.Empty(t, MissingDeleteRight(caps, "Apple"))
}

func TestMissingDeleteRightNamespacePageWithoutInterface(t *testing.T) {
	t.Parallel()

	caps := WikiCapabilities{UserRights: []string{"delete"}}
	require.Equal(t, RightEditInterface, MissingDeleteRight(caps, "MediaWiki:Sidebar"))
	// The first missing right wins; interface is checked before the suffix right.
	require.Equal(t, RightEditInterface, MissingDeleteRight(caps, "MediaWiki:Common.js"))
}

func TestDeleteAccessMessageMapsEachRight(t *testing.T) {
	t.Parallel()

	caps := sysopCaps()
	require.Empty(t, DeleteAccessMessage(caps, "MediaWiki:Sidebar"))
	require.Equal(t, SiteCSSDeleteGrantMessage, DeleteAccessMessage(caps, "MediaWiki:Common.css"))
	require.Equal(t, SiteJSDeleteGrantMessage, DeleteAccessMessage(caps, "MediaWiki:Common.js"))

	noInterface := WikiCapabilities{UserRights: []string{"delete"}}
	require.Equal(t, MediaWikiNamespaceDeleteGrantMessage, DeleteAccessMessage(noInterface, "MediaWiki:Sidebar"))

	noJSON := WikiCapabilities{UserRights: []string{"delete", "editinterface"}}
	wantMsg := SiteJSONDeleteGrantMessage
	require.Equal(t, wantMsg, DeleteAccessMessage(noJSON, "MediaWiki:Gadgets-definition.json"))
}

func TestFriendlyErrorMessageMapsSiteConfigCodes(t *testing.T) {
	t.Parallel()

	require.Equal(t, SiteCSSDeleteGrantMessage, FriendlyErrorMessage(&APIError{Code: SiteCSSProtectedErrorCode}))
	require.Equal(t, SiteJSDeleteGrantMessage, FriendlyErrorMessage(&APIError{Code: SiteJSProtectedErrorCode}))
	wantSiteConfigMsg := SiteJSONDeleteGrantMessage
	require.Equal(t, wantSiteConfigMsg, FriendlyErrorMessage(&APIError{Code: SiteJSONProtectedErrorCode}))
	// Unknown codes fall back to the API-provided info.
	require.Equal(t, "boom", FriendlyErrorMessage(&APIError{Code: "whatever", Info: "boom"}))
}
