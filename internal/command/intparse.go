package command

import "strconv"

// parseInt64 parses b with the rules of Redis's string2ll, which every
// integer argument and every INCR'd value goes through. It is stricter than
// strconv.ParseInt: a leading '+', any space, a leading zero (other than "0"
// itself) and "-0" are all rejected, so "007" or " 1" never count as numbers.
// Values outside the int64 range are rejected too.
func parseInt64(b []byte) (int64, bool) {
	if len(b) == 0 || len(b) > 20 {
		return 0, false
	}
	if len(b) == 1 && b[0] == '0' {
		return 0, true
	}
	digits := b
	if b[0] == '-' {
		digits = b[1:]
	}
	if len(digits) == 0 || digits[0] < '1' || digits[0] > '9' {
		return 0, false
	}
	for _, c := range digits[1:] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
