// Package timeline owns two things: trips, and the timeline assembled from a
// user's check-ins and points.
//
// A trip is user-owned data, never derived: nothing rebuilds one, and no
// config change invalidates it, unlike daily_stats and tracks. This package
// is the only intended caller of store.TripRepository, and it writes no
// derived state of its own, which is why it sits beside internal/ingest and
// internal/checkin in CLAUDE.md's layering rather than below them.
//
// The timeline itself is computed on read, never stored: Entries reads a
// range of check-ins and points and folds them into one ordered list, each
// entry carrying its own kind so a projection built on top — a compatibility
// endpoint, a screen — can tell them apart without a second engine of its
// own.
package timeline
