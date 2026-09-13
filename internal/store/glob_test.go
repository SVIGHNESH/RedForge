package store

import "testing"

func TestGlobTable(t *testing.T) {
	cases := []struct {
		pattern string
		s       string
		want    bool
	}{
		// Acceptance cases from T0.07.
		{`h[^e]llo`, "hallo", true},
		{`h[^e]llo`, "hello", false},
		{`h[a-b]llo`, "hallo", true},
		{`h[a-b]llo`, "hbllo", true},
		{`h[a-b]llo`, "hcllo", false},
		{`h\*llo`, "h*llo", true},
		{`h\*llo`, "hello", false},
		// Redis stringmatchlen behaviour.
		{`*`, "hello", true},
		{`*`, "", true},
		{`**`, "abc", true},
		{`hello`, "hello", true},
		{`hello`, "hell", false},
		{`h?llo`, "hello", true},
		{`h?llo`, "hllo", false},
		{`?`, "", false},
		{`h*llo`, "heeello", true},
		{`h*llo`, "hllo", true},
		{`h*llo`, "hll", false},
		{`*llo`, "hello", true},
		{`h*`, "hello", true},
		{`a*b*c`, "axbyc", true},
		{`a*b*c`, "axbyd", false},
		{`h[ae]llo`, "hallo", true},
		{`h[ae]llo`, "hello", true},
		{`h[ae]llo`, "hillo", false},
		{`h[a-z]llo`, "hmllo", true},
		{`h[a-z]llo`, "h1llo", false},
		{`h[^ae]llo`, "hillo", true},
		{`h[^ae]llo`, "hallo", false},
		{`h[!e]llo`, "hallo", true},
		{`h[!e]llo`, "hello", false},
		{`[-a]`, "-", true},
		{`[a-]`, "-", true},
		{`[a-]`, "a", true},
		{`[a-]`, "b", false},
		{`h\?llo`, "h?llo", true},
		{`h\?llo`, "hello", false},
		{`\\`, `\`, true},
		{`[abc]`, "b", true},
		{`[^abc]`, "d", true},
		{`[^abc]`, "b", false},
		// Unclosed bracket and trailing backslash degrade to literals.
		{`[abc`, "[abc", true},
		{`[abc`, "a", false},
		{`\`, `\`, true},
		{`\`, "a", false},
		// Case is significant.
		{`HELLO`, "hello", false},
		// Stars match across what ? and classes match singly.
		{`*a*`, "banana", true},
		{`*a*`, "bbb", false},
		{`user:*`, "user:1", true},
		{`user:*`, "other:1", false},
		{`?ser:1`, "user:1", true},
	}
	for _, tc := range cases {
		if got := Match(tc.pattern, tc.s); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

// Stars must backtrack over positions a class also matches: the first greedy
// attempt fails and only the retry succeeds.
func TestGlobStarBacktracksOverClass(t *testing.T) {
	if !Match(`*a[bc]`, "xaab") {
		t.Error(`Match("*a[bc]", "xaab") = false, want true`)
	}
	if Match(`*a[bc]`, "xaad") {
		t.Error(`Match("*a[bc]", "xaad") = true, want false`)
	}
}
