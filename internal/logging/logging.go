// Package logging provides a slog-based logger that fans out to daily JSON log
// files and an in-process queryable store, mirroring the former JsonLogStore.
package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry is a single stored log line, matching the former JSON schema.
type Entry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Module    string `json:"module"`
	Message   string `json:"message"`
}

// Store persists log entries to per-day files under logsDir and answers queries.
type Store struct {
	logsDir     string
	mu          sync.Mutex
	lastCleanup time.Time

	// file is the append handle for day, kept open across writes. The write path
	// runs once per log record — including every line OpenVPN prints — so the
	// open/close pair it used to do per line was two syscalls of pure overhead
	// under a lock every other writer needs.
	file *os.File
	day  string
}

// NewStore creates the logs directory and returns a Store.
func NewStore(logsDir string) (*Store, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{logsDir: logsDir}, nil
}

func (s *Store) write(level, module, message string, t time.Time) {
	entry := Entry{
		Timestamp: t.Local().Format("2006-01-02 15:04:05"),
		Level:     level,
		Module:    module,
		Message:   message,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	day := t.Local().Format("2006-01-02")
	s.mu.Lock()
	if f := s.appendFile(day); f != nil {
		_, _ = f.Write(append(line, '\n'))
	}
	s.mu.Unlock()
	s.cleanup(t)
}

// appendFile returns the open append handle for day, rotating to a new file when
// the date rolls over. Callers must hold s.mu.
func (s *Store) appendFile(day string) *os.File {
	if s.file != nil && s.day == day {
		return s.file
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file, s.day = nil, ""
	}
	f, err := os.OpenFile(filepath.Join(s.logsDir, day+".json"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	s.file, s.day = f, day
	return f
}

// Read returns stored entries for a date (default today), filtered by level and
// module substring, returning at most limit most-recent entries.
//
// The file is walked backwards from the end and the walk stops as soon as limit
// matches are in hand, so the cost is set by limit rather than by the size of
// the day's log. Reading it forwards meant materializing every entry in the file
// only to discard all but the last few — and the dashboard polls this endpoint
// every five seconds, against a file that a connected tunnel appends to
// continuously.
func (s *Store) Read(date, level, module string, limit int) []Entry {
	if date == "" {
		date = time.Now().Local().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return []Entry{}
	}
	if limit <= 0 {
		limit = defaultReadLimit
	}
	path := filepath.Join(s.logsDir, date+".json")
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(path)
	if err != nil {
		return []Entry{}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return []Entry{}
	}
	keep := func(e Entry) bool {
		if level != "" && !strings.EqualFold(e.Level, level) {
			return false
		}
		if module != "" && !strings.Contains(strings.ToLower(e.Module), strings.ToLower(module)) {
			return false
		}
		return true
	}
	return readTail(f, info.Size(), keep, limit)
}

const (
	// defaultReadLimit bounds an unlimited request. Read has no unbounded mode:
	// its whole purpose is to not hold a day of logs in memory at once.
	defaultReadLimit = 5000

	// tailChunk is how much of the file each backward step pulls in.
	tailChunk = 64 * 1024
)

// readTail collects up to limit entries satisfying keep, scanning backwards from
// the end of the file and returning them in chronological order.
func readTail(f *os.File, size int64, keep func(Entry) bool, limit int) []Entry {
	newestFirst := make([]Entry, 0, limit)
	// pending holds bytes the scan has read but not yet resolved into complete
	// lines: everything before the earliest newline seen so far.
	var pending []byte
	pos := size

	decode := func(line []byte) {
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if keep(e) {
			newestFirst = append(newestFirst, e)
		}
	}

	for pos > 0 && len(newestFirst) < limit {
		n := int64(tailChunk)
		if n > pos {
			n = pos
		}
		pos -= n
		chunk := make([]byte, n)
		if _, err := f.ReadAt(chunk, pos); err != nil {
			break
		}
		pending = append(chunk, pending...)
		for len(newestFirst) < limit {
			i := bytes.LastIndexByte(pending, '\n')
			if i < 0 {
				// What is left has no newline before it, so it is either the
				// first line of the file or a fragment continuing into the
				// previous chunk. Either way it is not complete yet.
				break
			}
			decode(pending[i+1:])
			pending = pending[:i]
		}
	}
	if pos == 0 && len(newestFirst) < limit && len(pending) > 0 {
		decode(pending) // first line of the file, which has no newline before it
	}

	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return newestFirst
}

func (s *Store) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < time.Hour {
		return
	}
	s.lastCleanup = now
	cutoff := now.Local().AddDate(0, 0, -3)
	entries, err := os.ReadDir(s.logsDir)
	if err != nil {
		return
	}
	for _, de := range entries {
		name := de.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(name, ".json"), time.Local)
		if err != nil {
			continue
		}
		if !day.After(cutoff.Truncate(24 * time.Hour)) {
			_ = os.Remove(filepath.Join(s.logsDir, name))
		}
	}
}

// storeHandler is a slog.Handler that writes each record into the Store while
// delegating formatted stderr output to a wrapped handler.
type storeHandler struct {
	store *Store
	next  slog.Handler
	attrs []slog.Attr
	group string
}

func (h *storeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *storeHandler) Handle(ctx context.Context, r slog.Record) error {
	module := "free_proxy"
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "module" {
			module = a.Value.String()
			return false
		}
		return true
	})
	h.store.write(strings.ToUpper(r.Level.String()), module, r.Message, r.Time)
	return h.next.Handle(ctx, r)
}

func (h *storeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &storeHandler{store: h.store, next: h.next.WithAttrs(attrs), group: h.group}
}

func (h *storeHandler) WithGroup(name string) slog.Handler {
	return &storeHandler{store: h.store, next: h.next.WithGroup(name), group: name}
}

// Configure installs a slog default logger that persists to the Store and also
// emits human-readable lines to stderr. Returns the Store for querying.
func Configure(logsDir string) (*Store, error) {
	store, err := NewStore(logsDir)
	if err != nil {
		return nil, err
	}
	text := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(&storeHandler{store: store, next: text}))
	return store, nil
}
