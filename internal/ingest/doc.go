// Package ingest is the single path through which points are inserted, updated
// and deleted.
//
// Every mutation of a point also rebuilds the affected days of daily_stats, in
// the same transaction as the mutation itself.
package ingest
