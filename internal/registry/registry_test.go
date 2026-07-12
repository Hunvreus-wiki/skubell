package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomCanonicalAndAPIURLNormalization(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		canonical string
		api       string
	}{
		{
			name:      "input with api.php",
			input:     "http://localhost:8080/w/api.php",
			canonical: "http://localhost:8080/w/",
			api:       "http://localhost:8080/w/api.php",
		},
		{
			name:      "input with trailing slash",
			input:     "http://localhost:8080/w/",
			canonical: "http://localhost:8080/w/",
			api:       "http://localhost:8080/w/api.php",
		},
		{
			name:      "input without trailing slash",
			input:     "http://localhost:8080/w",
			canonical: "http://localhost:8080/w/",
			api:       "http://localhost:8080/w/api.php",
		},
		{
			name:      "input with duplicate slashes",
			input:     "http://localhost:8080//w//api.php",
			canonical: "http://localhost:8080/w/",
			api:       "http://localhost:8080/w/api.php",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			entry := WikiEntry{Farm: "custom", CustomAPIURL: testCase.input}
			require.Equal(t, testCase.canonical, entry.CanonicalURL())
			require.Equal(t, testCase.api, entry.APIURL())
		})
	}
}

func TestWikimediaURLGeneration(t *testing.T) {
	tests := []struct {
		name      string
		entry     WikiEntry
		canonical string
		api       string
	}{
		{
			name:      "wikipedia fr",
			entry:     WikiEntry{Farm: "wikimedia", Family: "wikipedia", Language: "fr"},
			canonical: "https://fr.wikipedia.org/",
			api:       "https://fr.wikipedia.org/w/api.php",
		},
		{
			name:      "commons",
			entry:     WikiEntry{Farm: "wikimedia", Family: "commons"},
			canonical: "https://commons.wikimedia.org/",
			api:       "https://commons.wikimedia.org/w/api.php",
		},
		{
			name:      "test wikipedia",
			entry:     WikiEntry{Farm: "wikimedia", Family: "wikipedia", Language: "test"},
			canonical: "https://test.wikipedia.org/",
			api:       "https://test.wikipedia.org/w/api.php",
		},
		{
			name:      "test2 wikipedia",
			entry:     WikiEntry{Farm: "wikimedia", Family: "wikipedia", Language: "test2"},
			canonical: "https://test2.wikipedia.org/",
			api:       "https://test2.wikipedia.org/w/api.php",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.canonical, testCase.entry.CanonicalURL())
			require.Equal(t, testCase.api, testCase.entry.APIURL())
		})
	}
}

func TestFandomURLGeneration(t *testing.T) {
	english := WikiEntry{Farm: "fandom", WikiID: "starwars", Language: "en"}
	require.Equal(t, "https://starwars.fandom.com/", english.CanonicalURL())
	require.Equal(t, "https://starwars.fandom.com/api.php", english.APIURL())

	french := WikiEntry{Farm: "fandom", WikiID: "starwars", Language: "fr"}
	require.Equal(t, "https://starwars.fandom.com/fr/", french.CanonicalURL())
	require.Equal(t, "https://starwars.fandom.com/fr/api.php", french.APIURL())
}

func TestMirahezeAndWikiggURLGeneration(t *testing.T) {
	miraheze := WikiEntry{Farm: "miraheze", WikiID: "mywiki"}
	require.Equal(t, "https://mywiki.miraheze.org/", miraheze.CanonicalURL())
	require.Equal(t, "https://mywiki.miraheze.org/w/api.php", miraheze.APIURL())

	wikigg := WikiEntry{Farm: "wikigg", WikiID: "terraria"}
	require.Equal(t, "https://terraria.wiki.gg/", wikigg.CanonicalURL())
	require.Equal(t, "https://terraria.wiki.gg/api.php", wikigg.APIURL())
}

func TestDetectFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected DetectedWiki
	}{
		{
			name:  "wikimedia wikipedia with api path",
			input: "https://fr.wikipedia.org/w/api.php",
			expected: DetectedWiki{
				Farm:     "wikimedia",
				Family:   "wikipedia",
				Language: "fr",
			},
		},
		{
			name:  "wikimedia wikipedia root",
			input: "https://fr.wikipedia.org/",
			expected: DetectedWiki{
				Farm:     "wikimedia",
				Family:   "wikipedia",
				Language: "fr",
			},
		},
		{
			name:  "wikimedia test wikipedia",
			input: "https://test.wikipedia.org/w/api.php",
			expected: DetectedWiki{
				Farm:     "wikimedia",
				Family:   "wikipedia",
				Language: "test",
			},
		},
		{
			name:  "wikimedia test2 wikipedia",
			input: "https://test2.wikipedia.org/",
			expected: DetectedWiki{
				Farm:     "wikimedia",
				Family:   "wikipedia",
				Language: "test2",
			},
		},
		{
			name:  "commons",
			input: "https://commons.wikimedia.org/wiki/Main_Page",
			expected: DetectedWiki{
				Farm:   "wikimedia",
				Family: "commons",
			},
		},
		{
			name:  "fandom english",
			input: "https://starwars.fandom.com/wiki/Main_Page",
			expected: DetectedWiki{
				Farm:     "fandom",
				Language: "en",
				WikiID:   "starwars",
			},
		},
		{
			name:  "fandom with language path",
			input: "https://starwars.fandom.com/fr/wiki/Accueil",
			expected: DetectedWiki{
				Farm:     "fandom",
				Language: "fr",
				WikiID:   "starwars",
			},
		},
		{
			name:  "miraheze",
			input: "https://mywiki.miraheze.org/w/api.php",
			expected: DetectedWiki{
				Farm:   "miraheze",
				WikiID: "mywiki",
			},
		},
		{
			name:  "wikigg",
			input: "https://terraria.wiki.gg/wiki/Guide",
			expected: DetectedWiki{
				Farm:   "wikigg",
				WikiID: "terraria",
			},
		},
		{
			name:  "irregular wikimedia falls back to custom",
			input: "https://wikimania2024.wikimedia.org/",
			expected: DetectedWiki{
				Farm: "custom",
			},
		},
		{
			name:  "unknown host falls back to custom",
			input: "https://example.org/wiki/Main_Page",
			expected: DetectedWiki{
				Farm: "custom",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, DetectFromURL(testCase.input))
		})
	}
}
