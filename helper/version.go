package helper

import (
	"strconv"
	"strings"
)

func parseVersion(version string) ([3]uint64, bool) {
	var parsed [3]uint64
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		if len(part) == 0 || len(part) > 6 || (len(part) > 1 && part[0] == '0') {
			return parsed, false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return parsed, false
			}
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return parsed, false
		}
		parsed[i] = value
	}
	return parsed, true
}

func stableVersion(version string) bool {
	parsed, ok := parseVersion(version)
	return ok && parsed[0] > 0 && parsed[1]%2 == 0
}

func newerVersion(candidate, current string) bool {
	a, validA := parseVersion(candidate)
	b, validB := parseVersion(current)
	if !validA || !validB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
