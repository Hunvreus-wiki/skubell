package ops

import (
	"errors"
	"fmt"
)

// Revision contains normalized revision metadata used by planners.
type Revision struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	User      string `json:"user,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

// PageInfo contains normalized page metadata used by planners.
type PageInfo struct {
	Title      string            `json:"title"`
	PageID     int64             `json:"page_id,omitempty"`
	Exists     bool              `json:"exists"`
	Protection map[string]string `json:"protection,omitempty"`
}

// BlockInfo contains normalized block metadata.
type BlockInfo struct {
	User         string   `json:"user"`
	Blocked      bool     `json:"blocked"`
	Restrictions []string `json:"restrictions,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Expiry       string   `json:"expiry,omitempty"`
}

// UserInfo contains normalized connected-user metadata.
type UserInfo struct {
	Name   string   `json:"name"`
	Rights []string `json:"rights,omitempty"`
	Groups []string `json:"groups,omitempty"`
}

// SiteInfo contains normalized wiki metadata used by planners.
type SiteInfo struct {
	SiteName   string   `json:"site_name,omitempty"`
	Generator  string   `json:"generator,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

// DataProvider supplies data for read-phase operations.
type DataProvider interface {
	GetRevisions(title string) ([]Revision, error)
	GetPageInfo(title string) (*PageInfo, error)
	GetDeletedRevisions(title string) ([]Revision, error)
	// GetTalkPageTitle returns the associated talk page of a subject page, or "" when the title is itself a talk page or
	// has no talk namespace.
	GetTalkPageTitle(title string) (string, error)
	// GetSubjectPageTitle returns the subject page of a talk page, or "" when the title is not a talk page. It is the
	// inverse of GetTalkPageTitle and lets callers classify a title as subject vs talk.
	GetSubjectPageTitle(title string) (string, error)
	GetRedirects(title string) ([]string, error)
	// PagesExist reports, for each requested title, whether the page exists. Titles absent from the result map are
	// treated as non-existent.
	PagesExist(titles []string) (map[string]bool, error)
	GetBlockInfo(user string) (*BlockInfo, error)
	GetUserInfo() (*UserInfo, error)
	GetSiteInfo() (*SiteInfo, error)
}

// MockDataProvider is a fixture-backed DataProvider used by tests.
type MockDataProvider struct {
	Revisions        map[string][]Revision
	PageInfos        map[string]*PageInfo
	DeletedRevisions map[string][]Revision
	TalkPages        map[string]string
	SubjectPages     map[string]string
	Redirects        map[string][]string
	ExistingPages    map[string]struct{}
	BlockInfos       map[string]*BlockInfo
	UserInfoData     *UserInfo
	SiteInfoData     *SiteInfo
}

func (m *MockDataProvider) GetRevisions(title string) ([]Revision, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	revisions, ok := m.Revisions[title]
	if !ok {
		return nil, fmt.Errorf("revisions fixture not found for title %q", title)
	}
	return revisions, nil
}

func (m *MockDataProvider) GetPageInfo(title string) (*PageInfo, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	pageInfo, ok := m.PageInfos[title]
	if !ok {
		return nil, fmt.Errorf("page info fixture not found for title %q", title)
	}
	return pageInfo, nil
}

func (m *MockDataProvider) GetDeletedRevisions(title string) ([]Revision, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	revisions, ok := m.DeletedRevisions[title]
	if !ok {
		return nil, fmt.Errorf("deleted revisions fixture not found for title %q", title)
	}
	return revisions, nil
}

func (m *MockDataProvider) GetTalkPageTitle(title string) (string, error) {
	if m == nil {
		return "", errors.New("mock data provider is nil")
	}
	// A missing entry means "no associated talk page", matching the real provider.
	return m.TalkPages[title], nil
}

func (m *MockDataProvider) GetSubjectPageTitle(title string) (string, error) {
	if m == nil {
		return "", errors.New("mock data provider is nil")
	}
	// An empty result means "not a talk page"; a missing key is treated the same, so fixtures only list actual talk pages.
	return m.SubjectPages[title], nil
}

func (m *MockDataProvider) GetRedirects(title string) ([]string, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	// A missing entry means "no redirects", matching the real provider.
	return m.Redirects[title], nil
}

func (m *MockDataProvider) PagesExist(titles []string) (map[string]bool, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	result := make(map[string]bool, len(titles))
	for _, title := range titles {
		_, ok := m.ExistingPages[title]
		result[title] = ok
	}
	return result, nil
}

func (m *MockDataProvider) GetBlockInfo(user string) (*BlockInfo, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	blockInfo, ok := m.BlockInfos[user]
	if !ok {
		return nil, fmt.Errorf("block fixture not found for user %q", user)
	}
	return blockInfo, nil
}

func (m *MockDataProvider) GetUserInfo() (*UserInfo, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	if m.UserInfoData == nil {
		return nil, errors.New("user info fixture not found")
	}
	return m.UserInfoData, nil
}

func (m *MockDataProvider) GetSiteInfo() (*SiteInfo, error) {
	if m == nil {
		return nil, errors.New("mock data provider is nil")
	}
	if m.SiteInfoData == nil {
		return nil, errors.New("site info fixture not found")
	}
	return m.SiteInfoData, nil
}
