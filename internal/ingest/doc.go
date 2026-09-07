// Package ingest is the single path through which points are inserted, updated
// and deleted.
//
// Every mutation of a point also rebuilds the affected days of daily_stats,
// in the same transaction as the mutation itself, and enqueues a track
// rebuild for internal/track's own background worker to pick up.
// [Recalculate] is the daily_stats rebuild run over a user's whole history at
// once, for `travelmap recalculate`.
package ingest
