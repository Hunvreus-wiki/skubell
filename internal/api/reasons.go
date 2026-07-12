package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	reasonDelete    = "delete"
	reasonBlock     = "block"
	reasonProtect   = "protect"
	reasonRevDelete = "revdelete"
)

var reasonMessageByAction = map[string]string{
	reasonDelete:    "deletereason-dropdown",
	reasonBlock:     "ipbreason-dropdown",
	reasonProtect:   "protect-dropdown",
	reasonRevDelete: "revdelete-reason-dropdown",
}

// ReasonCategory represents a predefined reason group.
type ReasonCategory struct {
	Label   string   `json:"label"`
	Reasons []string `json:"reasons"`
}

// ReasonDropdown contains parsed reasons for one action.
type ReasonDropdown struct {
	Action     string           `json:"action"`
	Categories []ReasonCategory `json:"categories"`
}

// FetchReasonDropdowns loads and parses reason dropdown messages.
func FetchReasonDropdowns(client *Client, apiURL, adminLanguage string) (map[string]ReasonDropdown, error) {
	return FetchReasonDropdownsContext(context.Background(), client, apiURL, adminLanguage)
}

// FetchReasonDropdownsContext loads and parses reason dropdown messages.
func FetchReasonDropdownsContext(
	ctx context.Context,
	client *Client,
	apiURL, adminLanguage string,
) (map[string]ReasonDropdown, error) {
	messages := []string{
		reasonMessageByAction[reasonDelete],
		reasonMessageByAction[reasonBlock],
		reasonMessageByAction[reasonProtect],
		reasonMessageByAction[reasonRevDelete],
	}
	params := map[string]string{
		"action":        "query",
		"meta":          "allmessages",
		"ammessages":    strings.Join(messages, "|"),
		"formatversion": "2",
	}
	if strings.TrimSpace(adminLanguage) != "" {
		params["amlang"] = adminLanguage
		// Explicit language mode: request uncustomized translation messages.
		params["amcustomised"] = "unmodified"
	}

	response, err := client.GetContext(ctx, apiURL, params)
	if err != nil {
		return nil, fmt.Errorf("fetch reason dropdowns: %w", err)
	}
	return parseReasonDropdownResponse(response)
}

func parseReasonDropdownResponse(response map[string]any) (map[string]ReasonDropdown, error) {
	query, ok := response["query"].(map[string]any)
	if !ok {
		return nil, errors.New("missing query field in allmessages response")
	}
	allmessages, ok := query["allmessages"].([]any)
	if !ok {
		return nil, errors.New("missing query.allmessages in allmessages response")
	}

	byMessage := map[string]string{}
	for _, entry := range allmessages {
		message, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := message["name"].(string)
		content, _ := message["content"].(string)
		if content == "" {
			content, _ = message["*"].(string)
		}
		if name != "" {
			byMessage[strings.ToLower(name)] = content
		}
	}

	result := map[string]ReasonDropdown{}
	for action, message := range reasonMessageByAction {
		content := byMessage[message]
		result[action] = ReasonDropdown{
			Action:     action,
			Categories: parseReasonWikitext(content),
		}
	}
	return result, nil
}

func parseReasonWikitext(content string) []ReasonCategory {
	lines := strings.Split(content, "\n")
	categories := []ReasonCategory{}
	currentCategory := -1

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}

		if reason, ok := strings.CutPrefix(line, "**"); ok {
			reason = strings.TrimSpace(reason)
			if reason == "" {
				continue
			}
			if currentCategory < 0 {
				categories = append(categories, ReasonCategory{Label: "General", Reasons: []string{reason}})
				currentCategory = 0
				continue
			}
			categories[currentCategory].Reasons = append(categories[currentCategory].Reasons, reason)
			continue
		}

		if label, ok := strings.CutPrefix(line, "*"); ok {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			categories = append(categories, ReasonCategory{Label: label, Reasons: []string{}})
			currentCategory = len(categories) - 1
		}
	}

	filtered := make([]ReasonCategory, 0, len(categories))
	for _, category := range categories {
		if len(category.Reasons) == 0 {
			continue
		}
		filtered = append(filtered, category)
	}
	return filtered
}
