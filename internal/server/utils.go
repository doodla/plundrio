package server

import (
	"context"
	"fmt"

	"github.com/doodla/plundrio/internal/log"
)

// checkDiskQuota checks disk usage and handles quota warnings
func (s *Server) checkDiskQuota() (bool, error) {
	account, err := s.cachedAccountInfo(context.Background())
	if err != nil {
		return false, fmt.Errorf("failed to check disk quota: %w", err)
	}

	// Guard against Disk.Size == 0 (unlimited plans report 0, and a zero-value
	// Disk can come back from an API hiccup). Float division by zero would yield
	// NaN/+Inf — a Used>0 case would then log a bogus "over quota (+Inf% used)"
	// warning. With no known size we can't be over quota, so report false.
	if account.Disk.Size <= 0 {
		return false, nil
	}

	// Calculate usage percentage
	usagePercent := float64(account.Disk.Used) / float64(account.Disk.Size) * 100

	// Consider over quota if usage is above 95%
	isOverQuota := usagePercent >= 95

	if isOverQuota && !s.quotaWarning.Load() {
		log.Warn("server").Msgf("Put.io account is over quota (%.1f%% used)", usagePercent)
		s.quotaWarning.Store(true)
	} else if !isOverQuota && s.quotaWarning.Load() {
		// Reset warning when usage drops
		s.quotaWarning.Store(false)
	}

	return isOverQuota, nil
}
