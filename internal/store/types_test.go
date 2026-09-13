package store

import "testing"

func TestTypeConstantsAreDistinct(t *testing.T) {
	seen := map[Type]bool{}
	for _, typ := range []Type{TString, TList, THash, TSet, TZSet} {
		if typ == 0 {
			t.Error("zero Type must stay reserved so a missing type is detectable")
		}
		if seen[typ] {
			t.Errorf("duplicate Type value %d", uint8(typ))
		}
		seen[typ] = true
	}
}

func TestStringValCloneIsIdentity(t *testing.T) {
	v := &StringVal{B: []byte("hello")}
	// Strings are never mutated in place, so Clone returns the receiver and
	// the snapshot path shares the bytes without copying.
	if got := v.Clone(); got != Value(v) {
		t.Error("StringVal.Clone must return itself")
	}
	var nilVal *StringVal
	if nilVal.Clone() != nil {
		t.Error("nil StringVal.Clone must be nil")
	}
}

func TestHashValCloneIsIndependent(t *testing.T) {
	v := &HashVal{M: map[string][]byte{"f": []byte("v")}}
	clone, ok := v.Clone().(*HashVal)
	if !ok {
		t.Fatal("HashVal.Clone returned the wrong type")
	}
	clone.M["f"][0] = 'X'
	clone.M["new"] = []byte("n")
	if string(v.M["f"]) != "v" {
		t.Errorf("clone mutation leaked into original: %q", v.M["f"])
	}
	if _, ok := v.M["new"]; ok {
		t.Error("clone insertion leaked into original")
	}
}

func TestSetValCloneIsIndependent(t *testing.T) {
	v := &SetVal{M: map[string]struct{}{"a": {}}}
	clone, ok := v.Clone().(*SetVal)
	if !ok {
		t.Fatal("SetVal.Clone returned the wrong type")
	}
	clone.M["b"] = struct{}{}
	delete(clone.M, "a")
	if _, ok := v.M["a"]; !ok {
		t.Error("clone deletion leaked into original")
	}
	if _, ok := v.M["b"]; ok {
		t.Error("clone insertion leaked into original")
	}
}

func TestListAndZSetCloneAreNonNil(t *testing.T) {
	if (&ListVal{}).Clone() == nil {
		t.Error("ListVal.Clone must not be nil")
	}
	if (&ZSetVal{}).Clone() == nil {
		t.Error("ZSetVal.Clone must not be nil")
	}
}

func TestTTLStatusConstantsAreDistinct(t *testing.T) {
	if NoKey == NoExpire || NoKey == HasExpire || NoExpire == HasExpire {
		t.Error("TTLStatus values must be distinct")
	}
}
