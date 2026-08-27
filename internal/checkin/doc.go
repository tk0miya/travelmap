// Package checkin is the single path through which check-ins are written.
//
// The push webhook (Step 18) and the periodic fetch (Step 19) are the two
// collection paths, and both write through [Write]: they have to agree on
// how a duplicate is recognised and on which fields a repeat write
// overwrites, and a second writer would settle that twice. See "checkins" in
// docs/database.md for what the upsert keeps and what it refreshes.
//
// Unlike internal/ingest, a check-in touches no other table: it is neither a
// point nor a segment, so daily_stats is untouched by a write here.
package checkin
