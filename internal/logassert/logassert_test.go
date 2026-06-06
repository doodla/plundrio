package logassert

import (
	"fmt"
	"strings"
	"testing"

	"github.com/doodla/plundrio/internal/log"
)

func TestParseLineStructured(t *testing.T) {
	line := []byte(`{"level":"info","component":"transfers","time":"2026-06-05T12:00:00Z","message":"Found ready transfer","id":123,"name":"x"}`)
	ev, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if ev.Level != "info" || ev.Component != "transfers" || ev.Message != "Found ready transfer" {
		t.Errorf("unexpected parse: %+v", ev)
	}
	if ev.Time == "" {
		t.Error("time should be parsed")
	}
	// Non-reserved keys collapse into Fields.
	if ev.Fields["id"] == nil || ev.Fields["name"] == nil {
		t.Errorf("structured fields missing: %+v", ev.Fields)
	}
	// Reserved keys must NOT leak into Fields.
	for _, k := range []string{"level", "component", "time", "message"} {
		if _, present := ev.Fields[k]; present {
			t.Errorf("reserved key %q leaked into Fields", k)
		}
	}
}

func TestParseLineRejectsNonJSON(t *testing.T) {
	if _, err := ParseLine([]byte("not json")); err == nil {
		t.Error("ParseLine should reject a non-JSON line")
	}
	if _, err := ParseLine([]byte(`{"component":"x","message":"y"}`)); err == nil {
		t.Error("ParseLine should reject a line missing the level field")
	}
}

// TestStdoutTapPreservesStdout proves stdout is actually written: the tap (which
// captures what internal/log writes to stdout) sees the emitted lines. The
// console leg is zerolog's pretty ConsoleWriter (colored text, not JSON), so we
// assert on the message substrings rather than JSON-parsing the console output —
// JSON parsing (ParseLine/AssertStructured) is for the SSE `log` event payload,
// which the integration-verifier feeds from the real stream.
func TestStdoutTapPreservesStdout(t *testing.T) {
	tap := NewStdoutTap(log.LevelDebug)
	log.Info("transfers").Int("id", 1).Msg("first stdout line")
	log.Debug("download").Msg("second stdout line")
	tap.Restore()

	joined := strings.Join(toStrings(tap.Lines()), "\n")
	if joined == "" {
		t.Fatal("stdout was not written — tap captured nothing (stdout not preserved)")
	}
	if !strings.Contains(joined, "first stdout line") || !strings.Contains(joined, "second stdout line") {
		t.Errorf("expected both emitted messages in stdout, got:\n%s", joined)
	}
	// The console leg carries the component field (the value is separated from
	// "component=" by an ANSI reset, so match the key only).
	if !strings.Contains(joined, "component=") {
		t.Errorf("console line should carry the component field, got:\n%s", joined)
	}
}

// TestStdoutTapLevelGate proves raising the level suppresses lower-level lines:
// the debug line emitted after raising to warn must NOT appear.
func TestStdoutTapLevelGate(t *testing.T) {
	tap := NewStdoutTap(log.LevelDebug)

	log.Debug("download").Msg("debug-before-raise") // should appear
	tap.SetLevel(log.LevelWarn)                     // raise the gate
	log.Debug("download").Msg("debug-after-raise")  // gated out
	log.Info("transfers").Msg("info-after-raise")   // gated out
	log.Warn("transfers").Msg("warn-after-raise")   // should appear
	log.Error("transfers").Msg("error-after-raise") // should appear
	tap.Restore()

	lines := tap.Lines()
	joined := strings.Join(toStrings(lines), "\n")

	if !strings.Contains(joined, "debug-before-raise") {
		t.Error("debug line emitted while level=debug should be present")
	}
	if strings.Contains(joined, "debug-after-raise") {
		t.Error("level gate failed: debug line after raising to warn leaked into the stream")
	}
	if strings.Contains(joined, "info-after-raise") {
		t.Error("level gate failed: info line after raising to warn leaked into the stream")
	}
	if !strings.Contains(joined, "warn-after-raise") || !strings.Contains(joined, "error-after-raise") {
		t.Error("warn/error lines after raising the gate should still appear")
	}

	// AssertLevelGated against only the post-raise events would need event-time
	// correlation; here we assert the structural property directly: no debug
	// event survives once we filter to lines emitted after the raise. The
	// substring checks above already prove the gate; AssertLevelGated is
	// exercised in TestAssertLevelGated below against a controlled event set.
}

func TestAssertLevelGatedDetectsLeak(t *testing.T) {
	// A controlled set: one debug event present while the claimed gate is info.
	// AssertLevelGated must flag it. We run it against a recording TB so the
	// detection itself is the assertion.
	evs := []LogEvent{
		{Level: "info", Component: "x", Message: "ok"},
		{Level: "debug", Component: "x", Message: "leaked"},
	}
	rt := &recordingTB{}
	AssertLevelGated(rt, evs, "info")
	if len(rt.errs) == 0 {
		t.Fatal("AssertLevelGated failed to flag a debug event under an info gate")
	}
	if !strings.Contains(strings.Join(rt.errs, "\n"), "leaked") {
		t.Errorf("expected the leaked debug event to be flagged, got: %v", rt.errs)
	}

	// And it must pass a properly-gated set.
	clean := &recordingTB{}
	AssertLevelGated(clean, []LogEvent{{Level: "info"}, {Level: "warn"}}, "info")
	if len(clean.errs) != 0 {
		t.Errorf("AssertLevelGated flagged a clean set: %v", clean.errs)
	}
}

func TestAssertStructuredDetectsGarbage(t *testing.T) {
	rt := &recordingTB{}
	AssertStructured(rt, [][]byte{
		[]byte(`{"level":"info","component":"x","message":"ok"}`),
		[]byte(`this is not json`),
	})
	if len(rt.errs) == 0 {
		t.Fatal("AssertStructured failed to flag a non-JSON line")
	}
}

func toStrings(bs [][]byte) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b)
	}
	return out
}

// recordingTB records failures so the helper's own detection can be asserted.
type recordingTB struct{ errs []string }

func (r *recordingTB) Helper() {}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
