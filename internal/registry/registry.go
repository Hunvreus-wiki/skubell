package registry

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	wikimediaLanguageProjectRegex = regexp.MustCompile(
		`^([a-z0-9-]+)\.(wikipedia|wiktionary|wikibooks|wikinews|wikiquote|wikisource|wikiversity|wikivoyage)\.org$`,
	)
	fandomHostRegex   = regexp.MustCompile(`^([a-z0-9-]+)\.fandom\.com$`)
	mirahezeHostRegex = regexp.MustCompile(`^([a-z0-9-]+)\.miraheze\.org$`)
	wikiggHostRegex   = regexp.MustCompile(`^([a-z0-9-]+)\.wiki\.gg$`)
	languageCodeRegex = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8})?$`)
)

// WikiEntry stores wiki identification fields used to generate canonical and API URLs.
type WikiEntry struct {
	Name         string
	Farm         string
	Family       string
	Language     string
	WikiID       string
	CustomAPIURL string
}

// DetectedWiki describes the result of auto-identification from a URL.
type DetectedWiki struct {
	Farm     string
	Family   string
	Language string
	WikiID   string
}

// CanonicalURL returns the canonical wiki URL (without api.php, with trailing slash).
func (w WikiEntry) CanonicalURL() string {
	switch strings.ToLower(w.Farm) {
	case "wikimedia":
		return canonicalWikimediaURL(strings.ToLower(w.Family), strings.ToLower(w.Language))
	case "fandom":
		if w.WikiID == "" {
			return ""
		}
		language := strings.ToLower(w.Language)
		if language == "" || language == "en" {
			return "https://" + strings.ToLower(w.WikiID) + ".fandom.com/"
		}
		return "https://" + strings.ToLower(w.WikiID) + ".fandom.com/" + language + "/"
	case "miraheze":
		if w.WikiID == "" {
			return ""
		}
		return "https://" + strings.ToLower(w.WikiID) + ".miraheze.org/"
	case "wikigg":
		if w.WikiID == "" {
			return ""
		}
		return "https://" + strings.ToLower(w.WikiID) + ".wiki.gg/"
	case "custom":
		return normalizeCustomURL(w.CustomAPIURL)
	default:
		if w.CustomAPIURL == "" {
			return ""
		}
		return normalizeCustomURL(w.CustomAPIURL)
	}
}

// APIURL returns the full api.php endpoint URL for this wiki entry.
func (w WikiEntry) APIURL() string {
	canonicalURL := w.CanonicalURL()
	if canonicalURL == "" {
		return ""
	}

	switch strings.ToLower(w.Farm) {
	case "wikimedia", "miraheze":
		return canonicalURL + "w/api.php"
	default:
		return canonicalURL + "api.php"
	}
}

// DetectFromURL auto-identifies wiki farm/project/language/wiki id from an input URL.
func DetectFromURL(rawURL string) DetectedWiki {
	parsedURL, ok := parseURL(rawURL)
	if !ok {
		return DetectedWiki{Farm: "custom"}
	}

	host := strings.ToLower(hostWithoutPort(parsedURL.Host))
	pathSegments := nonEmptySegments(parsedURL.Path)

	if match := wikimediaLanguageProjectRegex.FindStringSubmatch(host); len(match) == 3 {
		return DetectedWiki{
			Farm:     "wikimedia",
			Family:   match[2],
			Language: match[1],
		}
	}

	switch host {
	case "commons.wikimedia.org":
		return DetectedWiki{Farm: "wikimedia", Family: "commons"}
	case "www.wikidata.org":
		return DetectedWiki{Farm: "wikimedia", Family: "wikidata"}
	case "species.wikimedia.org":
		return DetectedWiki{Farm: "wikimedia", Family: "wikispecies"}
	case "meta.wikimedia.org":
		return DetectedWiki{Farm: "wikimedia", Family: "meta"}
	case "www.mediawiki.org":
		return DetectedWiki{Farm: "wikimedia", Family: "mediawiki"}
	case "incubator.wikimedia.org":
		return DetectedWiki{Farm: "wikimedia", Family: "incubator"}
	case "www.wikifunctions.org":
		return DetectedWiki{Farm: "wikimedia", Family: "wikifunctions"}
	}

	if match := fandomHostRegex.FindStringSubmatch(host); len(match) == 2 {
		language := "en"
		if len(pathSegments) > 0 && languageCodeRegex.MatchString(strings.ToLower(pathSegments[0])) {
			language = strings.ToLower(pathSegments[0])
		}

		return DetectedWiki{
			Farm:     "fandom",
			Language: language,
			WikiID:   strings.ToLower(match[1]),
		}
	}

	if match := mirahezeHostRegex.FindStringSubmatch(host); len(match) == 2 {
		return DetectedWiki{
			Farm:   "miraheze",
			WikiID: strings.ToLower(match[1]),
		}
	}

	if match := wikiggHostRegex.FindStringSubmatch(host); len(match) == 2 {
		return DetectedWiki{
			Farm:   "wikigg",
			WikiID: strings.ToLower(match[1]),
		}
	}

	return DetectedWiki{Farm: "custom"}
}

func canonicalWikimediaURL(family, language string) string {
	switch family {
	case "wikipedia", "wiktionary", "wikibooks", "wikinews", "wikiquote", "wikisource", "wikiversity", "wikivoyage":
		if language == "" {
			return ""
		}
		return "https://" + language + "." + family + ".org/"
	case "commons":
		return "https://commons.wikimedia.org/"
	case "wikidata":
		return "https://www.wikidata.org/"
	case "wikispecies", "species":
		return "https://species.wikimedia.org/"
	case "meta":
		return "https://meta.wikimedia.org/"
	case "mediawiki":
		return "https://www.mediawiki.org/"
	case "incubator":
		return "https://incubator.wikimedia.org/"
	case "wikifunctions":
		return "https://www.wikifunctions.org/"
	default:
		return ""
	}
}

func normalizeCustomURL(rawURL string) string {
	parsedURL, ok := parseURL(rawURL)
	if !ok {
		return ""
	}

	decodedPath, err := url.PathUnescape(parsedURL.Path)
	if err != nil {
		decodedPath = parsedURL.Path
	}

	segments := nonEmptySegments(decodedPath)
	if len(segments) > 0 && strings.EqualFold(segments[len(segments)-1], "api.php") {
		segments = segments[:len(segments)-1]
	}

	parsedURL.Path = "/"
	if len(segments) > 0 {
		parsedURL.Path = "/" + strings.Join(segments, "/") + "/"
	}

	parsedURL.RawPath = ""
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""

	return parsedURL.String()
}

func parseURL(rawURL string) (*url.URL, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, false
	}

	parsedURL, err := url.Parse(trimmed)
	if err != nil {
		return nil, false
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, false
	}

	return parsedURL, true
}

func hostWithoutPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err == nil {
		return host
	}
	return hostPort
}

func nonEmptySegments(pathValue string) []string {
	rawSegments := strings.Split(pathValue, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return segments
}
