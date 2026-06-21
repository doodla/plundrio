package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/doodla/plundrio/internal/atomicfile"
	"github.com/spf13/viper"
)

// OverrideKeys are the canonical viper keys the runtime-overrides file may
// carry. They match the mapstructure tags on Config plus dashboard_addr, and
// are the only keys ApplyOverrides will push into viper / WriteOverride will
// persist. Anything else in the file is ignored (a forward-compat guard: an
// older daemon reading a newer file's key it doesn't understand just skips it).
var OverrideKeys = []string{
	"log_level",
	"workers",
	"target",
	"folder",
	"listen",
	"dashboard_addr",
}

// persistableKeySet is the set WriteOverride accepts: the boot-applied
// OverrideKeys PLUS token. Token is persistable (the contract's PUT reports it
// under "persisted") yet never boot-applied — ApplyOverrides iterates
// OverrideKeys (which omits token), so a stale or leaked file token can never
// become the resolved token.
var persistableKeySet = func() map[string]bool {
	m := make(map[string]bool, len(OverrideKeys)+1)
	for _, k := range OverrideKeys {
		m[k] = true
	}
	m["token"] = true
	return m
}()

// Overrides is the parsed runtime-overrides file: a sparse map of canonical key
// → value. Only keys the operator has actually edited are present; absent keys
// fall through to viper's other tiers (flag/env/config-file/default). Modeled as
// a map (not a struct) precisely so "absent" is distinguishable from "zero" —
// a struct with omitempty couldn't tell `workers: 0` from an unset workers.
type Overrides map[string]any

// EnvVarFor returns the PLDR_* environment variable name a canonical override
// key is pinned by. The mapping mirrors viper's SetEnvKeyReplacer("_"→"_") under
// the PLDR prefix: key "log_level" → "PLDR_LOG_LEVEL". This is the single place
// the env-pin check is derived, so the overrides guard and the settings
// "locked" report can't drift.
func EnvVarFor(key string) string {
	return "PLDR_" + strings.ToUpper(key)
}

// IsEnvPinned reports whether a canonical override key is pinned by its PLDR_*
// env var. A pinned key's override must be skipped (env wins) and is reported
// `locked` to the settings API.
func IsEnvPinned(key string) bool {
	_, present := os.LookupEnv(EnvVarFor(key))
	return present
}

// LoadOverrides reads and parses the runtime-overrides JSON file at path.
//
//   - A missing file is a no-op: returns (nil, nil), exactly like
//     CategoryStore.Load — the file is optional and absence means "no overrides."
//   - A present-but-malformed file is an ERROR, surfaced not swallowed: a
//     corrupt overrides file silently mis-resolving TargetDir/listen is the
//     blast-radius bug the design calls out, so the boot path must see it.
//
// Keys not in OverrideKeys are dropped on load (forward-compat).
func LoadOverrides(path string) (Overrides, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read overrides file %q: %w", path, err)
	}
	// Empty file is treated as no overrides (a freshly-touched file).
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse overrides file %q: %w", path, err)
	}

	ov := make(Overrides)
	for k, v := range raw {
		// Keep persistable keys (incl. token) so WriteOverride round-trips them;
		// ApplyOverrides iterates only OverrideKeys, so token is never
		// boot-applied even though it survives a load.
		if persistableKeySet[k] {
			ov[k] = v
		}
	}
	return ov, nil
}

// ApplyOverrides layers the overrides onto v via viper.Set — viper's highest
// (Override) tier — for every override key that is NOT env-pinned.
//
// The os.LookupEnv env-skip guard is load-bearing, not an optimization:
// viper.Set sits ABOVE AutomaticEnv, so applying an override for an env-pinned
// key would silently beat the env var, inverting the locked decision that env
// must win (the daemon can't rewrite compose). Skipping env-pinned keys
// preserves env-wins. See design §4.
//
// Returns the keys actually applied (env-pinned keys are reported skipped via
// their absence), for the caller to log if it wants visibility.
func ApplyOverrides(v *viper.Viper, ov Overrides) []string {
	applied := make([]string, 0, len(ov))
	for _, key := range OverrideKeys {
		val, present := ov[key]
		if !present {
			continue
		}
		if IsEnvPinned(key) {
			// Env wins; do NOT viper.Set, or the override would beat the env.
			continue
		}
		v.Set(key, val)
		applied = append(applied, key)
	}
	sort.Strings(applied)
	return applied
}

// writeMu serializes the load-merge-write in WriteOverride. The atomic rename
// prevents a torn file, but without this lock two concurrent writers (e.g. two
// dashboard tabs PUTting at once) would each read the same base, merge their own
// key, and one rename would clobber the other's update (lost update). Process-
// global because override writes are rare and always target the one file.
var writeMu sync.Mutex

// WriteOverride merges the given key/value pairs into the overrides file at
// path, preserving keys already present and not in updates. Only canonical
// OverrideKeys are written; unknown keys are rejected so a typo can't silently
// persist a dead key. Writes atomically (temp file + rename) with 0644, matching
// the CategoryStore persistence precedent's permissions. The load-merge-write is
// serialized (writeMu) so concurrent writers can't lose each other's updates.
//
// A nil/empty value for a key is rejected by callers (validation lives in the
// settings PUT handler); WriteOverride itself just persists what it's given.
func WriteOverride(path string, updates map[string]any) error {
	if path == "" {
		return fmt.Errorf("overrides file path is empty")
	}
	for k := range updates {
		if !persistableKeySet[k] {
			return fmt.Errorf("refusing to persist unknown override key %q", k)
		}
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	existing, err := LoadOverrides(path)
	if err != nil {
		return fmt.Errorf("load existing overrides before write: %w", err)
	}
	if existing == nil {
		existing = make(Overrides)
	}
	for k, val := range updates {
		existing[k] = val
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}

	// Atomic temp+rename (see atomicfile.WriteFile): a crash mid-write can't
	// corrupt the file, and concurrent writers (e.g. two dashboard tabs) don't
	// clobber each other's temp. writeMu above additionally serializes the
	// load-merge-write so neither loses the other's keys.
	if err := atomicfile.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write overrides: %w", err)
	}
	return nil
}
