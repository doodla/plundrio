package download

import (
	"errors"
	"fmt"
	"net/http"

	grab "github.com/cavaliergopher/grab/v3"
	"github.com/doodla/go-putio"
)

// DownloadError is the base error type for download-related errors
type DownloadError struct {
	Type    string
	Message string
}

// Error implements the error interface
func (e *DownloadError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// NewDownloadCancelledError creates a new error for cancelled downloads
func NewDownloadCancelledError(filename, reason string) error {
	return &DownloadError{
		Type:    "DownloadCancelled",
		Message: fmt.Sprintf("Download of %s was cancelled: %s", filename, reason),
	}
}

// NewTransferNotFoundError creates a new error for transfer not found situations
func NewTransferNotFoundError(transferID int64) error {
	return &DownloadError{
		Type:    "TransferNotFound",
		Message: fmt.Sprintf("Transfer ID %d not found", transferID),
	}
}

// NewNoFilesFoundError creates a new error for transfers with no files
func NewNoFilesFoundError(transferID int64) error {
	return &DownloadError{
		Type:    "NoFilesFound",
		Message: fmt.Sprintf("No files found for transfer %d", transferID),
	}
}

// isNotFoundError reports whether err is a put.io API 404. The API client
// wraps errors with fmt.Errorf("...: %w", err), so direct type assertion
// fails — errors.As walks the chain.
func isNotFoundError(err error) bool {
	var putioErr *putio.ErrorResponse
	if !errors.As(err, &putioErr) || putioErr.Response == nil {
		return false
	}
	return putioErr.Response.StatusCode == http.StatusNotFound
}

// retryableHTTPStatus reports whether err carries an HTTP status that warrants
// a download retry. Two typed error sources reach the download retry path:
// grab's StatusCodeError (the CDN file fetch) and put.io's ErrorResponse
// (GetDownloadURL). Matching the typed errors via errors.As — rather than
// substring-scanning the message for "429"/"502"/… — avoids false positives
// from a URL or byte count that merely contains those digits.
func retryableHTTPStatus(err error) bool {
	var code int
	var grabErr grab.StatusCodeError
	if errors.As(err, &grabErr) {
		code = int(grabErr)
	} else {
		var putioErr *putio.ErrorResponse
		if !errors.As(err, &putioErr) || putioErr.Response == nil {
			return false
		}
		code = putioErr.Response.StatusCode
	}
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}
