package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/doodla/go-putio"
)

// putioErrorWith builds a wrapped *putio.ErrorResponse with the given HTTP
// status and optional headers, matching the shape the real client returns
// (fmt.Errorf with %w). Request is populated because ErrorResponse.Error()
// dereferences it during formatting.
func putioErrorWith(statusCode int, header http.Header) error {
	req, _ := http.NewRequest(http.MethodGet, "https://api.put.io/v2/transfers/list", nil)
	resp := &http.Response{StatusCode: statusCode, Header: header, Request: req}
	return fmt.Errorf("get transfers: %w", &putio.ErrorResponse{Response: resp})
}

func rateLimitErr(retryAfter string) error {
	h := http.Header{}
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return putioErrorWith(http.StatusTooManyRequests, h)
}

func instantSleeper(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

// blockingSleeper never fires, forcing the withRateLimitRetry select to take
// the ctx.Done() arm — used to test cancellation deterministically.
func blockingSleeper(time.Duration) <-chan time.Time { return make(chan time.Time) }

func TestRetryAfterFromHeader(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-30 * time.Second).UTC().Format(http.TimeFormat)

	tests := []struct {
		name   string
		value  string
		min    time.Duration
		max    time.Duration
		absent bool
	}{
		{"seconds", "120", 120 * time.Second, 120 * time.Second, false},
		{"zero", "0", 0, 0, true},
		{"negative", "-5", 0, 0, true},
		{"empty", "", 0, 0, true},
		{"garbage", "soon", 0, 0, true},
		{"http_date_future", future, 25 * time.Second, 31 * time.Second, false},
		{"http_date_past", past, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.value != "" {
				h.Set("Retry-After", tt.value)
			}
			got := retryAfterFromHeader(h)
			if tt.absent {
				if got != 0 {
					t.Fatalf("want 0, got %v", got)
				}
				return
			}
			if got < tt.min || got > tt.max {
				t.Fatalf("want within [%v,%v], got %v", tt.min, tt.max, got)
			}
		})
	}
	if got := retryAfterFromHeader(nil); got != 0 {
		t.Fatalf("nil header: want 0, got %v", got)
	}
}

func TestBackoffDelay(t *testing.T) {
	// Server-supplied Retry-After wins and is exact (no jitter), capped.
	if got := backoffDelay(0, 5*time.Second); got != 5*time.Second {
		t.Fatalf("retry-after honored: want 5s, got %v", got)
	}
	if got := backoffDelay(0, 5*time.Minute); got != rlMaxBackoff {
		t.Fatalf("retry-after cap: want %v, got %v", rlMaxBackoff, got)
	}

	// Exponential growth with full jitter: delay in [base/2, base] per attempt.
	for attempt, base := range map[int]time.Duration{
		0: 1 * time.Second,
		1: 2 * time.Second,
		2: 4 * time.Second,
	} {
		for i := 0; i < 50; i++ {
			got := backoffDelay(attempt, 0)
			if got < base/2 || got > base {
				t.Fatalf("attempt %d: want [%v,%v], got %v", attempt, base/2, base, got)
			}
		}
	}

	// Large/overflowing attempt is capped at rlMaxBackoff (with jitter floor).
	got := backoffDelay(40, 0)
	if got < rlMaxBackoff/2 || got > rlMaxBackoff {
		t.Fatalf("overflow attempt: want [%v,%v], got %v", rlMaxBackoff/2, rlMaxBackoff, got)
	}
}

func TestIsRateLimited(t *testing.T) {
	if ra, ok := isRateLimited(nil); ok || ra != 0 {
		t.Fatalf("nil: want (0,false), got (%v,%v)", ra, ok)
	}
	if _, ok := isRateLimited(errors.New("boom")); ok {
		t.Fatalf("plain error: want false")
	}
	if _, ok := isRateLimited(putioErrorWith(http.StatusNotFound, nil)); ok {
		t.Fatalf("404: want false")
	}
	if _, ok := isRateLimited(&putio.ErrorResponse{Response: nil}); ok {
		t.Fatalf("nil response: want false")
	}
	ra, ok := isRateLimited(rateLimitErr(""))
	if !ok || ra != 0 {
		t.Fatalf("429 no header: want (0,true), got (%v,%v)", ra, ok)
	}
	ra, ok = isRateLimited(rateLimitErr("5"))
	if !ok || ra != 5*time.Second {
		t.Fatalf("429 with Retry-After: want (5s,true), got (%v,%v)", ra, ok)
	}
}

func TestWithRateLimitRetrySuccess(t *testing.T) {
	c := &Client{sleeper: instantSleeper}
	calls := 0
	err := c.withRateLimitRetry(context.Background(), "test", func() error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("want (nil,1 call), got (%v,%d)", err, calls)
	}
}

func TestWithRateLimitRetryRecovers(t *testing.T) {
	c := &Client{sleeper: instantSleeper}
	calls := 0
	err := c.withRateLimitRetry(context.Background(), "test", func() error {
		calls++
		if calls <= 2 {
			return rateLimitErr("")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("want (nil,3 calls), got (%v,%d)", err, calls)
	}
}

func TestWithRateLimitRetryExhausts(t *testing.T) {
	c := &Client{sleeper: instantSleeper}
	calls := 0
	err := c.withRateLimitRetry(context.Background(), "test", func() error {
		calls++
		return rateLimitErr("")
	})
	if _, ok := isRateLimited(err); !ok {
		t.Fatalf("want a 429 error returned, got %v", err)
	}
	if calls != rlMaxRetries+1 {
		t.Fatalf("want %d calls, got %d", rlMaxRetries+1, calls)
	}
}

func TestWithRateLimitRetryNon429Immediate(t *testing.T) {
	c := &Client{sleeper: instantSleeper}
	calls := 0
	sentinel := putioErrorWith(http.StatusInternalServerError, nil)
	err := c.withRateLimitRetry(context.Background(), "test", func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("want (sentinel,1 call), got (%v,%d)", err, calls)
	}
}

func TestWithRateLimitRetryContextCancelled(t *testing.T) {
	c := &Client{sleeper: blockingSleeper}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := c.withRateLimitRetry(ctx, "test", func() error {
		calls++
		return rateLimitErr("")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("want 1 call before cancel, got %d", calls)
	}
}

// TestWithRateLimitRetryPreservesErrorChain guards orphan-recovery: a 404
// surfaced through the wrapper must keep its %w chain so errors.As still
// unwraps the *putio.ErrorResponse (download.isNotFoundError depends on this).
func TestWithRateLimitRetryPreservesErrorChain(t *testing.T) {
	c := &Client{sleeper: instantSleeper}
	err := c.withRateLimitRetry(context.Background(), "test", func() error {
		return putioErrorWith(http.StatusNotFound, nil)
	})
	var putioErr *putio.ErrorResponse
	if !errors.As(err, &putioErr) || putioErr.Response.StatusCode != http.StatusNotFound {
		t.Fatalf("404 chain not preserved through wrapper: %v", err)
	}
}
