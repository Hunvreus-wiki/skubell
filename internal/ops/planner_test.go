package ops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockDataProviderReturnsFixtures(t *testing.T) {
	t.Parallel()

	mock := &MockDataProvider{
		Revisions: map[string][]Revision{
			"Apple": {{ID: 10, Timestamp: "2026-02-17T15:00:00Z"}},
		},
		PageInfos: map[string]*PageInfo{
			"Apple": {Title: "Apple", Exists: true, PageID: 123},
		},
		DeletedRevisions: map[string][]Revision{
			"Orange": {{ID: 20, Timestamp: "2026-02-17T15:01:00Z"}},
		},
		TalkPages: map[string]string{
			"Apple": "Talk:Apple",
		},
		Redirects: map[string][]string{
			"Apple": {"Apple (fruit)"},
		},
		BlockInfos: map[string]*BlockInfo{
			"TestBlocked": {User: "TestBlocked", Blocked: true, Restrictions: []string{"0"}},
		},
		UserInfoData: &UserInfo{Name: "TestAdmin", Rights: []string{"delete"}},
		SiteInfoData: &SiteInfo{SiteName: "Skubell Test Wiki"},
	}

	revs, err := mock.GetRevisions("Apple")
	require.NoError(t, err)
	require.Len(t, revs, 1)
	require.Equal(t, int64(10), revs[0].ID)

	page, err := mock.GetPageInfo("Apple")
	require.NoError(t, err)
	require.Equal(t, int64(123), page.PageID)

	deleted, err := mock.GetDeletedRevisions("Orange")
	require.NoError(t, err)
	require.Len(t, deleted, 1)

	talk, err := mock.GetTalkPageTitle("Apple")
	require.NoError(t, err)
	require.Equal(t, "Talk:Apple", talk)

	redirects, err := mock.GetRedirects([]string{"Apple"})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"Apple": {"Apple (fruit)"}}, redirects)

	block, err := mock.GetBlockInfo("TestBlocked")
	require.NoError(t, err)
	require.True(t, block.Blocked)

	userInfo, err := mock.GetUserInfo()
	require.NoError(t, err)
	require.Equal(t, "TestAdmin", userInfo.Name)

	siteInfo, err := mock.GetSiteInfo()
	require.NoError(t, err)
	require.Equal(t, "Skubell Test Wiki", siteInfo.SiteName)
}
