package command

import "github.com/SVIGHNESH/RedForge/internal/resp"

// Error wording shared by several handlers, verbatim from MASTER-PLAN
// Section 4.11 so tests and client tools see exactly what Redis sends.
const (
	msgWrongType = "WRONGTYPE Operation against a key holding the wrong kind of value"
	msgNotInt    = "ERR value is not an integer or out of range"
)

var (
	errWrongType = resp.Error(msgWrongType)
	errNotInt    = resp.Error(msgNotInt)
)
