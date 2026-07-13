// Package locales embeds the shipped translation files into the binary so they travel with the executable regardless
// of how it is packaged (fyne bundles, tarballs, `go run`), rather than being read from a directory on disk at runtime.
package locales

import "embed"

// FS holds the shipped active.<lang>.json translation files.
//
//go:embed active.*.json
var FS embed.FS
