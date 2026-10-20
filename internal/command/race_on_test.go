//go:build race

package command

// raceEnabled lets timing assertions stand down under the race detector,
// which slows every memory access by an order of magnitude.
const raceEnabled = true
