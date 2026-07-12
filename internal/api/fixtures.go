package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadFixture reads and parses a JSON fixture.
//
// Resolution order for relative paths:
// 1) given path as-is
// 2) testdata/fixtures/<path>
// 3) ../../testdata/fixtures/<path> (for package-local tests)
func LoadFixture(path string) (map[string]any, error) {
	if path == "" {
		return nil, errors.New("fixture path is empty")
	}

	resolvedPath, err := resolveFixturePath(path)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read fixture: %w", err)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse fixture json: %w", err)
	}
	return payload, nil
}

func resolveFixturePath(path string) (string, error) {
	candidates := []string{path}
	if !filepath.IsAbs(path) {
		candidates = append(candidates,
			filepath.Join("testdata", "fixtures", path),
			filepath.Join("..", "..", "testdata", "fixtures", path),
		)
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("fixture not found: %s", path)
}
