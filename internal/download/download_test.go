package download

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
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

		// Bare-string errors continue to be classified correctly.
		{name: "connection_reset", err: errors.New("connection reset"), want: true},
		{name: "connection_refused", err: errors.New("connection refused"), want: true},
		{name: "io_timeout", err: errors.New("i/o timeout"), want: true},
		{name: "http_429_too_many_requests", err: errors.New("HTTP 429 Too Many Requests"), want: true},
		{name: "server_returned_503", err: errors.New("server returned 503"), want: true},
		{name: "bad_gateway_502", err: errors.New("bad gateway 502"), want: true},
		{name: "gateway_timeout_504", err: errors.New("gateway timeout 504"), want: true},
		{name: "random_non_transient_error", err: errors.New("some random error"), want: false},

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
