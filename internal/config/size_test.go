package config

import "testing"

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"   ", 0},
		{"0", 0},
		{"1024", 1024},
		{"1024B", 1024},
		{"512 b", 512},
		{"10KB", 10_000},
		{"10MB", 10_000_000},
		{"2GB", 2_000_000_000},
		{"1TB", 1_000_000_000_000},
		{"1KiB", 1024},
		{"1MiB", 1 << 20},
		{"10GiB", 10 * (1 << 30)},
		{"1TiB", 1 << 40},
		{"1.5GiB", int64(1.5 * (1 << 30))},
		{"20 gb", 20_000_000_000}, // case-insensitive + space
	}
	for _, tt := range tests {
		got, err := ParseByteSize(tt.in)
		if err != nil {
			t.Errorf("ParseByteSize(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseByteSizeErrors(t *testing.T) {
	// "8388608TiB" == 2^23 * 2^40 == 2^63, the exact int64 overflow boundary
	// that float64 rounding would otherwise sneak past into a negative wrap.
	for _, in := range []string{"-1", "-5GB", "abc", "GB", "10XB", "10 PB", "1.2.3MB", "8388608TiB"} {
		if got, err := ParseByteSize(in); err == nil {
			t.Errorf("ParseByteSize(%q) = %d, want error", in, got)
		}
	}
}
