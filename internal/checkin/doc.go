// Package checkin is the single path through which check-ins are written.
//
// The push webhook and the periodic fetch are the two collection paths, and
// both write through [Write] — [WritePush] for the push webhook, which also
// parses the wire payload and resolves it onto a travelmap account first —
// so that however many collection paths feed it, they agree on how a
// duplicate is recognised and on which fields a repeat write overwrites. See
// "checkins" in docs/database.md for what the upsert keeps and what it
// refreshes.
//
// Unlike internal/ingest, a check-in touches no other table: it is neither a
// point nor a segment, so daily_stats is untouched by a write here.
package checkin
