package store

import (
	"context"
	"testing"
	"time"

	"github.com/masteralanlab/free-proxy/internal/domain"
)

func countRows(t *testing.T, repos *Repos, table string) int {
	t.Helper()
	var n int
	if err := repos.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestProbeResultsPruneByAge(t *testing.T) {
	ctx := context.Background()
	repos := newNodeRepos(t)
	now := time.Now().UTC()

	for i, at := range []time.Time{
		now.Add(-30 * 24 * time.Hour),
		now.Add(-8 * 24 * time.Hour),
		now.Add(-6 * 24 * time.Hour),
		now.Add(-time.Hour),
	} {
		res := domain.ProbeResult{
			NodeID:    "jp-node",
			Available: i%2 == 0,
			LatencyMS: 10 + i,
			ProbedAt:  at,
		}
		if _, err := repos.Probes.Insert(ctx, res); err != nil {
			t.Fatalf("insert probe %d: %v", i, err)
		}
	}
	if got := countRows(t, repos, "probe_results"); got != 4 {
		t.Fatalf("setup: got %d rows, want 4", got)
	}

	deleted, err := repos.Probes.DeleteOlderThan(ctx, now.Add(-HistoryRetention))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d rows, want 2", deleted)
	}
	if got := countRows(t, repos, "probe_results"); got != 2 {
		t.Errorf("%d rows remain, want 2", got)
	}

	// Pruning again is a no-op rather than an error.
	if deleted, err = repos.Probes.DeleteOlderThan(ctx, now.Add(-HistoryRetention)); err != nil || deleted != 0 {
		t.Errorf("second prune: deleted %d, err %v", deleted, err)
	}
}

// Old jobs go, but a job that is still pending or running stays whatever its
// recorded age — deleting the row a caller is polling would strand it.
func TestJobsPruneSkipsUnfinished(t *testing.T) {
	ctx := context.Background()
	repos := newNodeRepos(t)
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)

	if err := repos.Jobs.Create(ctx, "old-done", "maintenance", old); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.MarkSucceeded(ctx, "old-done", old, map[string]any{"probed": 1}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.Create(ctx, "old-failed", "probe-proxy", old); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.MarkFailed(ctx, "old-failed", old, "boom"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.Create(ctx, "old-pending", "probe-proxy", old); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.Create(ctx, "old-running", "probe-proxy", old); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.MarkRunning(ctx, "old-running", old); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.Create(ctx, "recent-done", "maintenance", now); err != nil {
		t.Fatal(err)
	}
	if err := repos.Jobs.MarkSucceeded(ctx, "recent-done", now, nil); err != nil {
		t.Fatal(err)
	}

	deleted, err := repos.Jobs.DeleteOlderThan(ctx, now.Add(-HistoryRetention))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d jobs, want 2 (the finished old ones)", deleted)
	}
	for _, id := range []string{"old-pending", "old-running", "recent-done"} {
		if _, err := repos.Jobs.Get(ctx, id); err != nil {
			t.Errorf("job %s should have survived: %v", id, err)
		}
	}
	for _, id := range []string{"old-done", "old-failed"} {
		if _, err := repos.Jobs.Get(ctx, id); err == nil {
			t.Errorf("job %s should have been pruned", id)
		}
	}
}
