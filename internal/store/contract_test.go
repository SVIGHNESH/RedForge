package store

import (
	"testing"
	"time"
)

// TestStoreContractCompiles pins every method of the frozen Store interface
// (docs/CONTRACTS.md Section 4.3) to its exact signature. Changing one makes
// this file fail to compile, which is the point: the contract changes only
// by a PR all four members approve.
func TestStoreContractCompiles(t *testing.T) {
	var (
		_ func(Store, string) (*Entry, bool)            = Store.Lookup
		_ func(Store, string) (*Entry, bool)            = Store.Mutable
		_ func(Store, string, *Entry)                   = Store.Put
		_ func(Store, string) bool                      = Store.Delete
		_ func(Store, string) bool                      = Store.Exists
		_ func(Store, string) []string                  = Store.Keys
		_ func(Store, string, int64) bool               = Store.SetExpireAt
		_ func(Store, string) bool                      = Store.Persist
		_ func(Store, string) (int64, TTLStatus)        = Store.TTLms
		_ func(Store) int                               = Store.Len
		_ func(Store) int                               = Store.ExpiringLen
		_ func(Store, int, time.Duration) int           = Store.SweepExpired
		_ func(Store) SnapshotView                      = Store.Snapshot
		_ func(Store, func(string, *Entry) bool)        = Store.ForEach
		_ func(SnapshotView, func(string, *Entry) bool) = SnapshotView.ForEach
		_ func(SnapshotView)                            = SnapshotView.Release
		_ func(Value) Value                             = Value.Clone
		_ func(Clock) int64                             = Clock.NowMs
		_ func(Clock) Store                             = New
	)
	e := Entry{Type: TString, Val: &StringVal{B: []byte("v")}, ExpireAt: 0}
	_ = []Type{TString, TList, THash, TSet, TZSet}
	_ = []TTLStatus{NoKey, NoExpire, HasExpire}
	_ = []Value{&StringVal{}, &ListVal{}, &HashVal{}, &SetVal{}, &ZSetVal{}}
	var _ Clock = RealClock{}
	if e.Type != TString {
		t.Fatal("unreachable")
	}
}
