package store

// Match reports whether s matches the Redis glob pattern, the same rule KEYS
// uses (MASTER-PLAN Section 4.3). Syntax, case-sensitive and byte-wise:
//
//   - any sequence, including empty
//     ?          any single byte
//     [abc]      any of the listed bytes
//     [a-z]      byte range
//     [^a]       negation; [!a] is accepted as an alias for Redis compatibility
//     \x         literal x (so \* \? \[ \\ match themselves)
//
// An unclosed [ and a trailing \ are literals. This is a port of Redis's
// stringmatchlen with nocase=0.
func Match(pattern, s string) bool {
	return matchLen(pattern, s)
}

func matchLen(pattern, s string) bool {
	px, sx := 0, 0
	nextPx, nextSx := -1, -1
	for sx < len(s) || px < len(pattern) {
		if px < len(pattern) {
			switch pattern[px] {
			case '*':
				// Collapse runs of stars, then remember the backtrack point:
				// try matching zero bytes first, consume one more on retry.
				for px+1 < len(pattern) && pattern[px+1] == '*' {
					px++
				}
				if px+1 == len(pattern) {
					return true
				}
				nextPx = px + 1
				nextSx = sx
				px++
				continue
			case '?':
				if sx < len(s) {
					px++
					sx++
					continue
				}
			case '[':
				end := classClose(pattern, px)
				if end < 0 {
					// Unclosed '[' is a literal.
					if sx < len(s) && s[sx] == '[' {
						px++
						sx++
						continue
					}
				} else if sx < len(s) && matchClass(pattern[px:], s[sx]) {
					px = end + 1
					sx++
					continue
				}
			case '\\':
				if px+1 < len(pattern) {
					if sx < len(s) && pattern[px+1] == s[sx] {
						px += 2
						sx++
						continue
					}
				} else if sx < len(s) && s[sx] == '\\' {
					// Trailing backslash is a literal backslash.
					px++
					sx++
					continue
				}
			default:
				if sx < len(s) && pattern[px] == s[sx] {
					px++
					sx++
					continue
				}
			}
		}
		// Mismatch: backtrack to the last star, if any, and let it eat one
		// more byte. Without a star the pattern fails here.
		if nextSx >= 0 && nextSx < len(s) {
			nextSx++
			sx = nextSx
			px = nextPx
			continue
		}
		return false
	}
	return true
}

// matchClass reports whether c matches the [...] class starting at pattern[0]
// == '['. An unclosed bracket never matches here; the caller in matchLen then
// falls through and treats '[' as a literal.
func matchClass(pattern string, c byte) bool {
	end := -1
	for i := 1; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
			continue
		}
		if pattern[i] == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return false
	}
	body := pattern[1:end]
	negated := false
	pos := 0
	if pos < len(body) && (body[pos] == '^' || body[pos] == '!') {
		negated = true
		pos++
	}
	matched := false
	for pos < len(body) {
		var lo byte
		if body[pos] == '\\' && pos+1 < len(body) {
			lo = body[pos+1]
			pos += 2
		} else {
			lo = body[pos]
			pos++
		}
		// A '-' between two literals is a range; first, last or escaped '-'
		// is a literal.
		if pos < len(body) && body[pos] == '-' && pos+1 < len(body) {
			var hi byte
			if body[pos+1] == '\\' && pos+2 < len(body) {
				hi = body[pos+2]
				pos += 3
			} else {
				hi = body[pos+1]
				pos += 2
			}
			if lo <= c && c <= hi {
				matched = true
			}
			continue
		}
		if lo == c {
			matched = true
		}
	}
	if negated {
		return !matched
	}
	return matched
}

// classClose returns the index of the closing ']' for the class opening at
// px, skipping escaped bytes, or -1 when the class is unclosed.
func classClose(pattern string, px int) int {
	for i := px + 1; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
			continue
		}
		if pattern[i] == ']' {
			return i
		}
	}
	return -1
}

// afterClass returns the pattern offset just past the closing ']' of the class
// at px. The caller only invokes it after matchClass found a closing bracket,
// but it defensively returns px+1 when none exists so '[' degrades to a
// literal step.
func afterClass(pattern string, px int) int {
	if end := classClose(pattern, px); end >= 0 {
		return end + 1
	}
	return px + 1
}
