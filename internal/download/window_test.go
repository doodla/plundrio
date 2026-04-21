package download

import (
	"testing"
	"time"

	"github.com/elsbrock/plundrio/internal/config"
)

func TestValidateStartWindow(t *testing.T) {
	tests := []struct {
		name    string
		window  config.DownloadStartWindowConfig
		wantErr bool
	}{
		{
			name:   "disabled_window_skips_validation",
			window: config.DownloadStartWindowConfig{},
		},
		{
			name: "valid_window",
			window: config.DownloadStartWindowConfig{
				Enabled: true,
				Start:   "23:00",
				End:     "05:00",
			},
		},
		{
			name: "invalid_start",
			window: config.DownloadStartWindowConfig{
				Enabled: true,
				Start:   "25:00",
				End:     "05:00",
			},
			wantErr: true,
		},
		{
			name: "invalid_end",
			window: config.DownloadStartWindowConfig{
				Enabled: true,
				Start:   "23:00",
				End:     "banana",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStartWindow(tt.window)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateStartWindow() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanStartDownloadNow(t *testing.T) {
	tests := []struct {
		name   string
		window config.DownloadStartWindowConfig
		now    string
		want   bool
	}{
		{
			name:   "disabled_window_allows_any_time",
			window: config.DownloadStartWindowConfig{},
			now:    "14:30",
			want:   true,
		},
		{
			name:   "inside_same_day_window",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "09:00", End: "17:00"},
			now:    "11:30",
			want:   true,
		},
		{
			name:   "outside_same_day_window",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "09:00", End: "17:00"},
			now:    "18:00",
			want:   false,
		},
		{
			name:   "inside_overnight_window_before_midnight",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "23:00", End: "05:00"},
			now:    "23:30",
			want:   true,
		},
		{
			name:   "inside_overnight_window_after_midnight",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "23:00", End: "05:00"},
			now:    "04:59",
			want:   true,
		},
		{
			name:   "outside_overnight_window",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "23:00", End: "05:00"},
			now:    "12:00",
			want:   false,
		},
		{
			name:   "equal_start_and_end_means_always_allowed",
			window: config.DownloadStartWindowConfig{Enabled: true, Start: "00:00", End: "00:00"},
			now:    "12:00",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.Parse("15:04", tt.now)
			if err != nil {
				t.Fatalf("time.Parse() error = %v", err)
			}

			if got := CanStartDownloadNow(tt.window, now); got != tt.want {
				t.Fatalf("CanStartDownloadNow() = %v, want %v", got, tt.want)
			}
		})
	}
}
