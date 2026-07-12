package ops

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// JournalEntry is an audit journal entry.
type JournalEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Module      string    `json:"module"`
	Operation   Operation `json:"operation"`
	Result      string    `json:"result"`
	ErrorCode   string    `json:"error_code,omitempty"`
	ErrorDetail string    `json:"error,omitempty"`
}

// AppendToJournal appends a single JSON line to the journal file.
func AppendToJournal(path string, entry JournalEntry) error {
	if path == "" {
		return errors.New("journal path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create journal directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal journal entry: %w", err)
	}
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write journal entry: %w", err)
	}
	return nil
}

// ReadJournal reads all JSONL entries from a journal file.
func ReadJournal(path string) ([]JournalEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []JournalEntry{}, nil
		}
		return nil, fmt.Errorf("open journal file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	entries := make([]JournalEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("decode journal entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan journal file: %w", err)
	}

	return entries, nil
}
