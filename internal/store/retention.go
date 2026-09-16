package store

import (
	"context"
	"time"

	"github.com/masteralanlab/free-proxy/internal/domain"
)

// HistoryRetention is how long probe and job history is kept.
//
// Neither table had a retention rule, and both only ever grew. probe_results is
// the expensive one: every row carries the whole ProbeResult as JSON, including
// the 50-line OpenVPN log tail, and a maintenance cycle writes one per node it
// probes. At the default budget that is a few megabytes a day, forever, on a box
// whose database also holds the live pool.
//
// A week is well past the point where an individual probe still says anything
// about a node — the node's own counters carry the durable verdict — and still
// long enough to look back over an incident.
//
// SQLite does not hand freed pages back to the filesystem without a VACUUM, so
// pruning bounds the database rather than shrinking it. Bounding is the point:
// the reclaimed pages are reused by everything written afterwards.
const HistoryRetention = 7 * 24 * time.Hour

// DeleteOlderThan drops probe results recorded before cutoff.
func (r *ProbeResultRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM probe_results WHERE probed_at < ?`, tstr(cutoff))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteOlderThan drops finished jobs created before cutoff. Jobs still pending
// or running are left alone whatever their age — nothing else is old enough to
// still be running by accident, and Initialize cancels the ones a crash stranded.
func (r *JobRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM jobs WHERE created_at < ? AND status NOT IN (?, ?)`,
		tstr(cutoff), string(domain.JobPending), string(domain.JobRunning))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
