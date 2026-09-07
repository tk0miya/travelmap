// Package timeline owns trips: a time range a user declares — a title and a
// range, nothing more.
//
// A trip is user-owned data, never derived: nothing rebuilds one, and no
// config change invalidates it, unlike daily_stats and tracks. This package
// is the only intended caller of store.TripRepository, and it writes no
// derived state of its own, which is why it sits beside internal/ingest and
// internal/checkin in CLAUDE.md's layering rather than below them.
package timeline
