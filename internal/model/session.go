package model

import "time"

// Session is a browser session scs hands out, a different credential from a
// [User]'s APIKey: it expires, POST /logout destroys it, and one account may
// hold several at once.
type Session struct {
	// Token is what scs keys the session by, hashed before it reaches here —
	// see internal/store/sqlite/migrations/0006_sessions.sql for why.
	Token string

	// Data is scs's own gob-encoded session data. Nothing outside scs decodes
	// it, which is also why there is no field here naming the session's user.
	Data []byte

	Expiry time.Time
}
