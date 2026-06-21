package download

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"

	grab "github.com/cavaliergopher/grab/v3"
)

func TestIsTransientError(t *testing.T) {
	// grabStyleConnReset mirrors what cavaliergopher/grab returns when the
	// remote drops a connection mid-stream: the syscall error wrapped through
	// os.SyscallError → net.OpError → fmt.Errorf with the request URL prefix.
	// This is the exact shape that the original exact-string classifier missed.
	grabStyleConnReset := fmt.Errorf(
		`Get "https://example.put.io/file": %w`,
		&net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
		},
	)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil_error", err: nil, want: false},
		{name: "download_cancelled_error", err: NewDownloadCancelledError("test.mkv", "shutdown"), want: false},

		// Bare-string network errors continue to be classified correctly.
		{name: "connection_reset", err: errors.New("connection reset"), want: true},
		{name: "connection_refused", err: errors.New("connection refused"), want: true},
		{name: "io_timeout", err: errors.New("i/o timeout"), want: true},
		{name: "random_non_transient_error", err: errors.New("some random error"), want: false},

		// Retryable HTTP status codes are matched on grab's typed
		// StatusCodeError (the real CDN download error shape), not by
		// substring-scanning the message.
		{name: "grab_429", err: grab.StatusCodeError(http.StatusTooManyRequests), want: true},
		{name: "grab_502", err: grab.StatusCodeError(http.StatusBadGateway), want: true},
		{name: "grab_503", err: grab.StatusCodeError(http.StatusServiceUnavailable), want: true},
		{name: "grab_504", err: grab.StatusCodeError(http.StatusGatewayTimeout), want: true},
		{name: "grab_404_not_retryable", err: grab.StatusCodeError(http.StatusNotFound), want: false},
		{
			name: "grab_503_wrapped",
			err:  fmt.Errorf("download failed: %w", grab.StatusCodeError(http.StatusServiceUnavailable)),
			want: true,
		},
		// put.io API 5xx from GetDownloadURL reaches the same retry path.
		{name: "putio_503", err: putioErrorWith("get download url", http.StatusServiceUnavailable), want: true},
		{name: "putio_500_not_retryable", err: putioErrorWith("get download url", http.StatusInternalServerError), want: false},

		// A bare-string error that merely contains "429"/"502" is NO longer
		// treated as transient — only the typed status errors above are. This
		// guards against false positives from URLs or byte counts.
		{name: "bare_string_with_429_not_transient", err: errors.New("downloaded 4294967296 bytes"), want: false},

		// Wrapped messages — the original classifier failed these.
		{
			name: "wrapped_connection_reset",
			err:  fmt.Errorf("request failed: %w", errors.New("connection reset")),
			want: true,
		},
		{
			name: "grab_style_connection_reset_wraps_syscall",
			err:  grabStyleConnReset,
			want: true,
		},
		{
			name: "errors_is_econnreset",
			err:  fmt.Errorf("download failed: %w", syscall.ECONNRESET),
			want: true,
		},
		{
			name: "errors_is_etimedout",
			err:  fmt.Errorf("download failed: %w", syscall.ETIMEDOUT),
			want: true,
		},
		{
			name: "broken_pipe",
			err:  fmt.Errorf("write tcp: %w", syscall.EPIPE),
			want: true,
		},
		{
			name: "unexpected_eof",
			err:  errors.New("download failed: unexpected EOF"),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientError(tt.err)
			if got != tt.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
