package ops

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WikiIdentity identifies the wiki a journal belongs to. It is written to wiki_identity.json so the SHA-1 subdirectory
// name can be resolved back to a wiki, and it never contains a password.
type WikiIdentity struct {
	URL      string `json:"url"`
	Farm     string `json:"farm"`
	Family   string `json:"family,omitempty"`
	Language string `json:"language,omitempty"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
}

// SessionJournal appends a session's JournalEntry records to a per-wiki JSONL file under a journal root. The wiki
// subdirectory (SHA-1 of the canonical URL), the wiki_identity.json file, and the timestamped session file are all
// created lazily on the first Append, so an idle connection leaves nothing behind. It is not safe for concurrent use.
type SessionJournal struct {
	root     string
	identity WikiIdentity
	now      func() time.Time
	path     string // session file path; empty until the first Append initializes it
}

// NewSessionJournal creates a journal writer rooted at root for the wiki identified by identity. Nothing is written
// until the first Append. A nil now defaults to time.Now (now is injectable so tests get deterministic file names).
func NewSessionJournal(root string, identity WikiIdentity, now func() time.Time) *SessionJournal {
	if now == nil {
		now = time.Now
	}
	return &SessionJournal{root: root, identity: identity, now: now}
}

// Append writes entry as one JSONL line, creating the subdirectory, wiki_identity.json, and session file on first call.
func (j *SessionJournal) Append(entry JournalEntry) error {
	if j == nil {
		return nil
	}
	if j.path == "" {
		if err := j.start(); err != nil {
			return err
		}
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal journal entry: %w", err)
	}
	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file: %w", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write journal entry: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close journal file: %w", err)
	}
	return nil
}

// start creates the wiki subdirectory, (over)writes wiki_identity.json, and picks the session file name.
func (j *SessionJournal) start() error {
	dir := filepath.Join(j.root, wikiSubdir(j.identity.URL))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create journal directory: %w", err)
	}
	payload, err := json.MarshalIndent(j.identity, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal wiki identity: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wiki_identity.json"), payload, 0o644); err != nil {
		return fmt.Errorf("write wiki identity: %w", err)
	}
	j.path = sessionFilePath(dir, j.now().UTC())
	return nil
}

// wikiSubdir is the per-wiki directory name: the hex SHA-1 of the canonical URL. SHA-1 is used only as a stable,
// fixed-length, filesystem-safe identifier, not for security.
func wikiSubdir(canonicalURL string) string {
	sum := sha1.Sum([]byte(canonicalURL))
	return hex.EncodeToString(sum[:])
}

// sessionFilePath names the session file by the RFC 3339 UTC timestamp with colons replaced by hyphens, adding a
// numeric suffix (-2, -3, …) if a file with that name already exists (two sessions starting in the same second).
func sessionFilePath(dir string, start time.Time) string {
	base := strings.ReplaceAll(start.UTC().Format(time.RFC3339), ":", "-")
	candidate := filepath.Join(dir, base+".jsonl")
	for n := 2; fileExists(candidate); n++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", base, n))
	}
	return candidate
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
