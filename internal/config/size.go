package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseByteSize parses a human-readable size string into a byte count.
//
// Accepted forms (case-insensitive, optional space before the unit):
//
//	""              -> 0      (the documented "disabled / unset" value)
//	"1024", "1024B" -> 1024 bytes
//	"10KB"/"10MB"/"10GB"/"10TB"    -> decimal (×1000)
//	"10KiB"/"10MiB"/"10GiB"/"10TiB" -> binary  (×1024)
//
// Fractional values ("1.5GiB") are allowed and rounded down to whole bytes.
// Negative, malformed, or unknown-unit inputs are rejected. The decimal/binary
// split matches the convention operators expect from `df`-style tooling: GB is
// 10^9, GiB is 2^30.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Split the leading numeric run from the trailing unit. The number may
	// carry a sign and a decimal point; everything after is the unit.
	i := 0
	for i < len(s) && (s[i] == '.' || s[i] == '-' || s[i] == '+' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	numStr := strings.TrimSpace(s[:i])
	unit := strings.ToUpper(strings.TrimSpace(s[i:]))
	if numStr == "" {
		return 0, fmt.Errorf("invalid size %q: missing numeric value", s)
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid size %q: must not be negative", s)
	}

	var mult float64
	switch unit {
	case "", "B":
		mult = 1
	case "KB":
		mult = 1e3
	case "MB":
		mult = 1e6
	case "GB":
		mult = 1e9
	case "TB":
		mult = 1e12
	case "KIB":
		mult = 1 << 10
	case "MIB":
		mult = 1 << 20
	case "GIB":
		mult = 1 << 30
	case "TIB":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("invalid size %q: unknown unit %q", s, unit)
	}

	bytes := val * mult
	// float64 can't represent math.MaxInt64 exactly — it rounds up to 2^63 — so
	// both sides promote to the same 2^63 float and ">=" rejects the exact-2^63
	// case (which would otherwise wrap negative through int64()). Anything below
	// rounds to a representable value <= MaxInt64-1024 and converts safely.
	if bytes >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q: exceeds maximum", s)
	}
	return int64(bytes), nil
}
