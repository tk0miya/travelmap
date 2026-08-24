// Package ingest is the single path through which points are inserted, updated
// and deleted.
//
// Every mutation of a point also rebuilds the affected days of daily_stats, in
// the same transaction as the mutation itself. [Recalculate] is the same
// rebuild run over a user's whole history at once, for `travelmap recalculate`.
package ingest
