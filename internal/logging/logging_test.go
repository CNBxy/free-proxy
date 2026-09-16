package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func today() string { return time.Now().Local().Format("2006-01-02") }

func TestReadReturnsMostRecentEntriesInOrder(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for i := 0; i < 500; i++ {
		s.write("INFO", "openvpn", fmt.Sprintf("line %d", i), now)
	}

	got := s.Read("", "", "", 3)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	for i, want := range []string{"line 497", "line 498", "line 499"} {
		if got[i].Message != want {
			t.Errorf("entry %d: got %q, want %q", i, got[i].Message, want)
		}
	}
}

// A message longer than one backward chunk must survive being split across
// reads, and so must the very first line of the file, which has no newline
// before it.
func TestReadSpansChunkBoundariesAndReachesFirstLine(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	huge := strings.Repeat("x", 3*tailChunk)
	s.write("INFO", "first", "oldest", now)
	s.write("INFO", "big", huge, now)
	s.write("INFO", "last", "newest", now)

	got := s.Read("", "", "", 10)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Message != "oldest" || got[2].Message != "newest" {
		t.Fatalf("wrong order: %q .. %q", got[0].Message, got[2].Message)
	}
	if got[1].Message != huge {
		t.Errorf("long line was truncated: got %d bytes, want %d", len(got[1].Message), len(huge))
	}
}

// The limit counts entries that survive filtering, not lines scanned.
func TestReadFiltersBeforeApplyingLimit(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	for i := 0; i < 200; i++ {
		s.write("INFO", "noise", fmt.Sprintf("noise %d", i), now)
	}
	for i := 0; i < 3; i++ {
		s.write("WARN", "gateway", fmt.Sprintf("warn %d", i), now)
	}
	for i := 0; i < 200; i++ {
		s.write("INFO", "noise", fmt.Sprintf("more noise %d", i), now)
	}

	got := s.Read("", "warn", "", 2)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].Message != "warn 1" || got[1].Message != "warn 2" {
		t.Errorf("got %q, %q", got[0].Message, got[1].Message)
	}

	byModule := s.Read("", "", "GATE", 10)
	if len(byModule) != 3 {
		t.Fatalf("module filter: want 3 entries, got %d", len(byModule))
	}
}

func TestReadHandlesMissingAndEmptyFiles(t *testing.T) {
	s := newTestStore(t)
	if got := s.Read("2020-01-01", "", "", 10); len(got) != 0 {
		t.Errorf("missing file: got %d entries", len(got))
	}
	if got := s.Read("not-a-date", "", "", 10); len(got) != 0 {
		t.Errorf("bad date: got %d entries", len(got))
	}
	if err := os.WriteFile(filepath.Join(s.logsDir, today()+".json"), nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if got := s.Read("", "", "", 10); len(got) != 0 {
		t.Errorf("empty file: got %d entries", len(got))
	}
}

// Garbage lines are skipped rather than ending the scan, so entries older than
// them stay reachable.
func TestReadSkipsUnparsableLines(t *testing.T) {
	s := newTestStore(t)
	s.write("INFO", "svc", "good one", time.Now())
	path := filepath.Join(s.logsDir, today()+".json")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("not json at all\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()

	got := s.Read("", "", "", 10)
	if len(got) != 1 || got[0].Message != "good one" {
		t.Fatalf("got %+v", got)
	}
}

// The append handle is held open across writes; a date rollover must close it
// and start the next day's file rather than keep appending to the old one.
func TestWriteRotatesOnDateChange(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	s.write("INFO", "svc", "yesterday's line", yesterday)
	s.write("INFO", "svc", "today's line", now)

	prev := s.Read(yesterday.Local().Format("2006-01-02"), "", "", 10)
	if len(prev) != 1 || prev[0].Message != "yesterday's line" {
		t.Fatalf("previous day: got %+v", prev)
	}
	cur := s.Read("", "", "", 10)
	if len(cur) != 1 || cur[0].Message != "today's line" {
		t.Fatalf("current day: got %+v", cur)
	}
}
