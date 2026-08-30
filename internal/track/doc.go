// Package track is the single path through which tracks are computed.
//
// A track is a contiguous run of a user's points, split from its neighbours
// by a gap exceeding TRAVELMAP_TRACK_BREAK_MINUTES of inactivity. Every one
// of a user's tracks is rebuilt from scratch whenever a point changes
// anywhere in that user's history — never adjusted arithmetically — because a
// single new point can shift where every later boundary falls; [RebuildUser]
// is that rebuild.
//
// internal/ingest enqueues a rebuild request through store.TrackRepository
// whenever it writes a point, and [RunWorker] is the background job that
// drains those requests — the first genuine consumer of the "Background
// work" job table described in docs/architecture.md. [RecalculateAll] runs
// the same rebuild over every user at once, for `travelmap recalculate`,
// which is what a TRAVELMAP_TRACK_BREAK_MINUTES change needs: it touches no
// point, so it enqueues nothing on its own.
package track
