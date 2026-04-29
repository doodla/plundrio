package download

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/elsbrock/go-putio"
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
