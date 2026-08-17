// Package dto holds the JSON structs exchanged with Dawarich clients.
//
// Compatibility with the upstream Dawarich API lives here: field names, their
// casing (/stats is camelCase, everything else is snake_case), and request body
// wrapping. Nothing in here carries business logic.
package dto
