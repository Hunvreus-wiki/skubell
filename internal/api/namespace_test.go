package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTitleHasNamespaceUsesConfiguredNameAliasAndFallback(t *testing.T) {
	t.Parallel()

	caps := WikiCapabilities{
		Namespaces: map[int]string{
			MediaWikiNamespaceID: "Project messages",
		},
		NamespaceAliases: map[int][]string{
			MediaWikiNamespaceID: {"MediaWiki"},
		},
	}

	require.True(t, TitleHasNamespace(caps, "Project messages:Sidebar", MediaWikiNamespaceID, "MediaWiki"))
	require.True(t, TitleHasNamespace(caps, "MediaWiki:Sidebar", MediaWikiNamespaceID, "MediaWiki"))
	require.False(t, TitleHasNamespace(caps, "Help:Sidebar", MediaWikiNamespaceID, "MediaWiki"))
	require.False(t, TitleHasNamespace(caps, "Sidebar", MediaWikiNamespaceID, "MediaWiki"))
}

func TestTitleHasNamespaceFallsBackWhenSiteInfoMissing(t *testing.T) {
	t.Parallel()

	require.True(t, IsMediaWikiNamespaceTitle(WikiCapabilities{}, "MediaWiki:Common.css"))
	require.False(t, IsMediaWikiNamespaceTitle(WikiCapabilities{}, "MediaWiki talk:Common.css"))
}

func TestCanDeleteMediaWikiNamespaceRequiresEditInterface(t *testing.T) {
	t.Parallel()

	require.False(t, CanDeleteMediaWikiNamespace(WikiCapabilities{UserRights: []string{"delete", "undelete"}}))
	require.True(t, CanDeleteMediaWikiNamespace(WikiCapabilities{UserRights: []string{"delete", "editinterface"}}))
}

func TestFriendlyErrorMessageMapsProtectedNamespaceInterface(t *testing.T) {
	t.Parallel()

	got := FriendlyErrorMessage(&APIError{
		Code: ProtectedNamespaceInterfaceErrorCode,
		Info: "This page provides interface text for the software on this wiki.",
	})

	require.Equal(t, MediaWikiNamespaceDeleteGrantMessage, got)
}
