package api

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/doodla/go-putio"
	"github.com/doodla/plundrio/internal/log"
)

// Rate-limit retry tuning. These govern how api.Client reacts to put.io HTTP
// 429 (Too Many Requests) responses on the JSON API path. The file-download
// (grab/CDN) path has its own transient-error handling in
// internal/download/download.go and is unaffected.
const (
	rlBaseBackoff = 1 * time.Second
	rlMaxBackoff  = 60 * time.Second
	// rlMaxRetries is the number of retries AFTER the initial attempt. Once
	// exhausted the last 429 error is returned to the caller so the failure
	// surfaces (logged, monitor tick dropped) rather than blocking forever.
	// Kept modest so a sustained throttle can't wedge the single monitor
	// goroutine for long — the 30s poll loop provides a coarse retry on top.
	rlMaxRetries = 3
)

// isRateLimited reports whether err is a put.io HTTP 429 and, if so, the
// Retry-After delay parsed from the response headers (0 when absent or
// unparseable). It walks the wrap chain with errors.As — the same pattern as
// download.isNotFoundError — because every api.Client method wraps its error
// with fmt.Errorf("...: %w", err).
func isRateLimited(err error) (retryAfter time.Duration, ok bool) {
	var putioErr *putio.ErrorResponse
	if !errors.As(err, &putioErr) || putioErr.Response == nil {
		return 0, false
	}
	if putioErr.Response.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return retryAfterFromHeader(putioErr.Response.Header), true
}

// retryAfterFromHeader parses a Retry-After header value, which per RFC 7231 is
// EITHER an integer number of seconds ("120") OR an HTTP-date. Returns 0 when
// the header is absent or unparseable, and clamps a past HTTP-date to 0.
func retryAfterFromHeader(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// backoffDelay computes the delay before retry attempt n (0-based) using
// exponential backoff with full jitter, capped at rlMaxBackoff. A server-
// supplied Retry-After takes precedence (we honor the server) but is still
// capped at rlMaxBackoff so a garbage/hostile header can't wedge the caller.
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > rlMaxBackoff {
			return rlMaxBackoff
		}
		return retryAfter
	}
	// base * 2^attempt, capped. attempt is bounded by rlMaxRetries so the shift
	// cannot overflow, but guard defensively against a negative/oversized value.
	d := rlMaxBackoff
	if attempt >= 0 && attempt < 32 {
		if shifted := rlBaseBackoff << uint(attempt); shifted > 0 && shifted < rlMaxBackoff {
			d = shifted
		}
	}
	// Full jitter: uniformly random in [d/2, d].
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

// withRateLimitRetry runs fn, retrying on a put.io HTTP 429 with exponential
// backoff plus jitter (honoring Retry-After when present). Non-429 errors and
// success return immediately. Backoff respects ctx cancellation between
// attempts. After rlMaxRetries are exhausted it returns the last 429 error so
// callers still observe a failure instead of blocking indefinitely.
//
// Why per-method wrapping rather than a transport-level http.RoundTripper
// (which would cover every call automatically): a retried request must replay
// its body, and UploadFile's body is a consumed io.Reader. The underlying
// go-putio client doesn't set Request.GetBody, so a RoundTripper couldn't
// safely re-send an upload — whereas wrapping at this layer lets UploadFile
// rebuild its bytes.Reader per attempt (see client.go). The cost is that a new
// client method must opt in by wrapping its call; that's the deliberate
// trade. Do NOT double-wrap recursive helpers (see GetAllTransferFiles).
func (c *Client) withRateLimitRetry(ctx context.Context, op string, fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		retryAfter, limited := isRateLimited(err)
		if !limited {
			return err
		}
		if attempt >= rlMaxRetries {
			log.Error("api").Str("op", op).Int("attempts", attempt+1).
				Msg("put.io rate limit retries exhausted; surfacing error to caller")
			return err
		}
		delay := backoffDelay(attempt, retryAfter)
		log.Warn("api").
			Str("op", op).
			Int("attempt", attempt+1).
			Int("max_attempts", rlMaxRetries+1).
			Dur("backoff", delay).
			Dur("retry_after", retryAfter).
			Msg("put.io rate limited (HTTP 429); backing off")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.sleeper(delay):
		}
	}
}
