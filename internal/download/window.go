package download

import (
	"fmt"
	"strings"
	"time"

	"github.com/elsbrock/plundrio/internal/config"
)

// ValidateStartWindow checks whether the configured download-start window is valid.
func ValidateStartWindow(window config.DownloadStartWindowConfig) error {
	if !window.Enabled {
		return nil
	}

	if _, err := parseClockMinutes(window.Start); err != nil {
		return fmt.Errorf("invalid download_start_window.start: %w", err)
	}

	if _, err := parseClockMinutes(window.End); err != nil {
		return fmt.Errorf("invalid download_start_window.end: %w", err)
	}

	return nil
}

// CanStartDownloadNow reports whether a new local download may start at the supplied time.
// Existing downloads are unaffected.
func CanStartDownloadNow(window config.DownloadStartWindowConfig, now time.Time) bool {
	if !window.Enabled {
		return true
	}

	startMinutes, err := parseClockMinutes(window.Start)
	if err != nil {
		return false
	}

	endMinutes, err := parseClockMinutes(window.End)
	if err != nil {
		return false
	}

	nowMinutes := now.Hour()*60 + now.Minute()

	if startMinutes == endMinutes {
		return true
	}

	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}

	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

func parseClockMinutes(value string) (int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("expected HH:MM 24-hour time: %q", value)
	}

	return parsed.Hour()*60 + parsed.Minute(), nil
}
