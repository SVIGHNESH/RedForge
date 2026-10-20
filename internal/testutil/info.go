package testutil

import (
	"fmt"
	"strings"
)

// InfoReply is an INFO payload as ordered sections of key:value fields.
type InfoReply struct {
	// Sections lists the "# Name" headers in output order.
	Sections []string
	// Keys lists each section's field names in output order.
	Keys map[string][]string
	// Values maps every field name to its value, across all sections.
	Values map[string]string
}

// ParseInfo parses an INFO payload strictly: CRLF line endings, "# Name"
// section headers, key:value lines, and single blank lines only between
// sections. Anything else is an error, so a malformed INFO fails the test
// that reads it instead of being half-read.
func ParseInfo(payload string) (InfoReply, error) {
	info := InfoReply{Keys: map[string][]string{}, Values: map[string]string{}}
	if payload == "" {
		return info, nil
	}
	if !strings.HasSuffix(payload, "\r\n") {
		return info, fmt.Errorf("INFO payload does not end in CRLF: %q", payload)
	}
	lines := strings.Split(strings.TrimSuffix(payload, "\r\n"), "\r\n")
	cur := ""
	for i, line := range lines {
		switch {
		case strings.ContainsAny(line, "\r\n"):
			return info, fmt.Errorf("INFO line %d has a bare CR or LF: %q", i, line)
		case strings.HasPrefix(line, "# "):
			if i > 0 && lines[i-1] != "" {
				return info, fmt.Errorf("INFO section %q is not preceded by a blank line", line)
			}
			cur = line[2:]
			info.Sections = append(info.Sections, cur)
		case line == "":
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "# ") {
				return info, fmt.Errorf("INFO blank line %d is not followed by a section header", i)
			}
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok || cur == "" {
				return info, fmt.Errorf("INFO line %d %q is not key:value inside a section", i, line)
			}
			if _, dup := info.Values[k]; dup {
				return info, fmt.Errorf("INFO field %q appears twice", k)
			}
			info.Keys[cur] = append(info.Keys[cur], k)
			info.Values[k] = v
		}
	}
	return info, nil
}
