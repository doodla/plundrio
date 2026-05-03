package main

import (
	"fmt"
	"os"
	"strings"
)

// resolveOAuthToken returns the put.io OAuth token, preferring an explicit
// `token` value (from flag, env, or config file) over a token-file path.
//
// PLDR_TOKEN_FILE is the systemd-credentials integration: the NixOS module
// sets `LoadCredential=token:<path>` and exposes the credential dir via
// `PLDR_TOKEN_FILE=%d/token`. Without this resolver, the env var was
// advertised but ignored — plundrio started with an empty token and died
// in Authenticate.
//
// Behavior:
//   - both empty: return ""; main.go's required-values check rejects it.
//   - explicit token only: return as-is.
//   - tokenFile only: read the file, trim trailing whitespace.
//   - both set: explicit wins (env layering convention).
//   - tokenFile set but file missing/unreadable: return error so the caller
//     fails loudly rather than silently falling back to an empty token.
func resolveOAuthToken(explicit, tokenFile string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if tokenFile == "" {
		return "", nil
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("read token file %q: %w", tokenFile, err)
	}
	return strings.TrimRight(string(data), "\r\n \t"), nil
}
