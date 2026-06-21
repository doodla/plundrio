package main

import (
	"testing"
	"time"

	"github.com/doodla/plundrio/internal/config"
)

func TestValidateRunConfig(t *testing.T) {
	base := func() *config.Config { return &config.Config{} }

	tests := []struct {
		name     string
		cfg      *config.Config
		demoMode bool
		wantErr  bool
	}{
		{"empty defaults", base(), false, false},
		{"zero interval allowed", &config.Config{TransferCheckInterval: 0}, false, false},
		{"5s interval allowed", &config.Config{TransferCheckInterval: 5 * time.Second}, false, false},
		{"sub-5s interval rejected outside demo", &config.Config{TransferCheckInterval: time.Second}, false, true},
		{"sub-5s interval allowed in demo", &config.Config{TransferCheckInterval: time.Second}, true, false},
		{"valid min_free_space", &config.Config{MinFreeSpace: "20GB"}, false, false},
		{"empty min_free_space allowed", &config.Config{MinFreeSpace: ""}, false, false},
		{"bad min_free_space rejected", &config.Config{MinFreeSpace: "10 PB"}, false, true},
		{
			"bad start window rejected",
			&config.Config{DownloadStartWindow: config.DownloadStartWindowConfig{Enabled: true, Start: "99:99", End: "05:00"}},
			false,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRunConfig(tt.cfg, tt.demoMode)
			if tt.wantErr && err == nil {
				t.Errorf("validateRunConfig() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateRunConfig() = %v, want nil", err)
			}
		})
	}
}
