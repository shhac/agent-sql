// Package errors provides error classification for database errors. The
// FixableBy taxonomy is re-exported from the shared lib-agent-output contract
// so the whole agent-* family classifies errors identically; the QueryError
// type and its hints stay local because they're agent-sql domain policy
// (driver-specific hints, connection-not-found shaping), not part of the
// generic wire contract.
//
// This file holds only the aliased boundary to lib-agent-output. The local
// domain (QueryError and its constructors) lives in query_error.go.
package errors

import (
	out "github.com/shhac/lib-agent-output"
)

// FixableBy indicates who can fix an error. It is the shared contract type, so
// the string values ("agent"/"human"/"retry") match the rest of the family.
type FixableBy = out.FixableBy

const (
	FixableByAgent = out.FixableByAgent
	FixableByHuman = out.FixableByHuman
	FixableByRetry = out.FixableByRetry
)
