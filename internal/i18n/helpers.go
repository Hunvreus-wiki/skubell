package i18n

import (
	"bytes"
	"maps"
	"strings"
	"text/template"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// T translates a simple message, returning the English fallback when no translation is available.
func T(id, fallback string) string {
	return localize(&goi18n.Message{ID: id, Other: fallback}, 0, nil, fallback)
}

// Td translates a message containing template variables such as {{.Wiki}}.
func Td(id, fallback string, data map[string]any) string {
	return localize(&goi18n.Message{ID: id, Other: fallback}, 0, data, renderTemplate(fallback, data))
}

// Tp translates a message with singular/plural forms selected by count.
func Tp(id, one, other string, count int) string {
	data := map[string]any{"Count": count}
	return localize(
		&goi18n.Message{ID: id, One: one, Other: other},
		count,
		data,
		renderTemplate(pluralFallback(one, other, count), data),
	)
}

// Tpd translates a message with plural forms and additional template variables.
func Tpd(id, one, other string, count int, data map[string]any) string {
	merged := make(map[string]any, len(data)+1)
	maps.Copy(merged, data)
	merged["Count"] = count
	fallback := renderTemplate(pluralFallback(one, other, count), merged)
	return localize(&goi18n.Message{ID: id, One: one, Other: other}, count, merged, fallback)
}

// localize runs go-i18n with the given default message. A non-empty One marks a plural message, for which PluralCount
// selects the CLDR form. If the active language's selected form is absent from the translation (e.g. Breton's two/few
// forms when a file provides only one/other), it falls back to the translation's "other" form rather than leaking the
// English default; only a total miss returns the pre-rendered English fallback.
func localize(msg *goi18n.Message, count int, data map[string]any, fallback string) string {
	loc := active()
	if msg.One == "" {
		if translated, err := loc.Localize(
			&goi18n.LocalizeConfig{DefaultMessage: msg, TemplateData: data},
		); err == nil &&
			translated != "" {
			return translated
		}
		return fallback
	}

	if translated, err := loc.Localize(&goi18n.LocalizeConfig{
		DefaultMessage: msg, TemplateData: data, PluralCount: count,
	}); err == nil && translated != "" {
		return translated
	}
	// Retry with the "other" form (no PluralCount) so a translation that omits a CLDR category still resolves in its
	// own language.
	if translated, err := loc.Localize(&goi18n.LocalizeConfig{
		DefaultMessage: &goi18n.Message{ID: msg.ID, Other: msg.Other}, TemplateData: data,
	}); err == nil && translated != "" {
		return translated
	}
	return fallback
}

// renderTemplate applies text/template data to text, returning text unchanged when it has no placeholders or fails.
func renderTemplate(text string, data map[string]any) string {
	if data == nil || !strings.Contains(text, "{{") {
		return text
	}
	tmpl, err := template.New("i18n").Parse(text)
	if err != nil {
		return text
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return text
	}
	return buf.String()
}

// pluralFallback picks the English form for the fallback path (an English-only one/other heuristic).
func pluralFallback(one, other string, count int) string {
	if count == 1 {
		return one
	}
	return other
}
