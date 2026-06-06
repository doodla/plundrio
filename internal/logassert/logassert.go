// Package logassert is the log-stream assertion helper the integration-verifier
// uses to prove the dashboard's SSE log stream behaves: (a) it emits STRUCTURED
// events (parseable JSON with the contract's level/component/message/fields
// shape), (b) it RESPECTS the active level (raising the level via the settings
// PUT suppresses lower-level lines), and (c) stdout is still written.
//
// Two layers:
//
//   - LogEvent + ParseLine: the structured shape the SSE `log` event carries,
//     parsed from a zerolog JSON line. The dashboard hub will parse the same
//     bytes the same way, so this doubles as a contract check on the log line.
//
//   - StdoutTap: a self-test fixture that captures what internal/log writes to
//     stdout (by swapping os.Stdout for a pipe and re-running the logger's
//     configuration via log.SetLevel). It proves, NOW and without the M4 hub,
//     that level changes gate the stream and that stdout still receives lines.
//     The integration-verifier reuses ParseLine/LogEvent against the real SSE
//     stream; StdoutTap is the local self-test substrate.
package logassert

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/doodla/plundrio/internal/log"
)

// LogEvent is the parsed form of one log line, matching the SSE `log` event
// payload in the contract: time/level/component/message plus the remaining
// structured fields verbatim.
type LogEvent struct {
	Time      string         `json:"time"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields"`
}

// reservedKeys are the zerolog top-level keys lifted into named LogEvent fields;
// everything else collapses into Fields.
var reservedKeys = map[string]bool{
	"time": true, "level": true, "component": true, "message": true,
}

// ParseLine parses one zerolog JSON line into a LogEvent. The zerolog base
// encoding is JSON (the ConsoleWriter only pretty-prints the console leg), so a
// raw line looks like {"level":"info","component":"x","time":"...","message":"..",...}.
// Remaining keys become Fields. Returns an error if the line isn't a JSON object
// or lacks a level (the minimum a structured event must carry).
func ParseLine(line []byte) (LogEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return LogEvent{}, fmt.Errorf("log line is not JSON: %w", err)
	}
	ev := LogEvent{Fields: map[string]any{}}
	for k, v := range raw {
		switch k {
		case "time":
			ev.Time, _ = v.(string)
		case "level":
			ev.Level, _ = v.(string)
		case "component":
			ev.Component, _ = v.(string)
		case "message":
			ev.Message, _ = v.(string)
		default:
			if !reservedKeys[k] {
				ev.Fields[k] = v
			}
		}
	}
	if ev.Level == "" {
		return LogEvent{}, fmt.Errorf("log line missing required \"level\" field: %s", line)
	}
	return ev, nil
}

// AssertStructured parses every line and fails via t if any line isn't a
// well-formed structured event. Returns the parsed events. Use this against the
// real SSE log stream's data payloads (one JSON object per `log` event) or
// against StdoutTap output.
func AssertStructured(t TB, lines [][]byte) []LogEvent {
	t.Helper()
	out := make([]LogEvent, 0, len(lines))
	for i, line := range lines {
		ev, err := ParseLine(line)
		if err != nil {
			t.Errorf("line %d not a structured log event: %v", i, err)
			continue
		}
		if ev.Component == "" {
			t.Errorf("line %d missing component (every log.* call adds one): %s", i, line)
		}
		if ev.Message == "" {
			t.Errorf("line %d missing message: %s", i, line)
		}
		out = append(out, ev)
	}
	return out
}

// AssertLevelGated fails if any event in evs is below minLevel — i.e. the active
// level did not gate the stream. levelRank orders the zerolog levels; an event
// whose rank is below the minimum should never have reached the stream.
func AssertLevelGated(t TB, evs []LogEvent, minLevel string) {
	t.Helper()
	min, ok := levelRank[minLevel]
	if !ok {
		t.Fatalf("AssertLevelGated: unknown minLevel %q", minLevel)
	}
	for _, ev := range evs {
		r, ok := levelRank[ev.Level]
		if !ok {
			continue // debug-level zerolog lines without a known rank: ignore
		}
		if r < min {
			t.Errorf("level gate leaked: event at level %q present but active level is %q (%q msg)",
				ev.Level, minLevel, ev.Message)
		}
	}
}

// levelRank orders levels low→high so a gate at `info` admits info/warn/error
// but not debug.
var levelRank = map[string]int{
	"debug": 0, "info": 1, "warn": 2, "error": 3, "fatal": 4,
}

// StdoutTap captures what internal/log writes to stdout. It swaps os.Stdout for
// an os.Pipe and re-runs the logger config (via log.SetLevel) so the package
// logger writes into the pipe. Restore() puts os.Stdout back and re-binds the
// logger to it.
//
// This exists because internal/log captures os.Stdout at configure time; a tap
// must therefore reconfigure AFTER swapping the file. It is the self-test
// substrate for "stdout is preserved" and "level changes gate the stream"
// without depending on the (M4) SSE hub.
type StdoutTap struct {
	mu      sync.Mutex
	orig    *os.File
	r, w    *os.File
	lines   [][]byte
	done    chan struct{}
	stopped bool
}

// NewStdoutTap installs the tap at the given level and starts draining the pipe.
// Callers MUST defer Restore(). The level argument is applied via log.SetLevel,
// which both sets the global zerolog level and rebinds the console writer to the
// (now swapped) os.Stdout.
func NewStdoutTap(level log.LogLevel) *StdoutTap {
	r, w, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("logassert: os.Pipe: %v", err))
	}
	tap := &StdoutTap{
		orig: os.Stdout,
		r:    r,
		w:    w,
		done: make(chan struct{}),
	}
	os.Stdout = w
	// Rebind the package logger to the swapped stdout AND set the level.
	log.SetLevel(level)

	go func() {
		defer close(tap.done)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			b := make([]byte, len(sc.Bytes()))
			copy(b, sc.Bytes())
			tap.mu.Lock()
			tap.lines = append(tap.lines, b)
			tap.mu.Unlock()
		}
	}()
	return tap
}

// SetLevel raises/lowers the active level mid-capture (simulating the settings
// PUT that widens/narrows the live log stream). Lines emitted after this call
// are gated by the new level.
func (tp *StdoutTap) SetLevel(level log.LogLevel) {
	log.SetLevel(level)
}

// Lines returns a snapshot of the JSON log lines captured so far. Because the
// drain goroutine is async, call Sync() first if you need every line emitted up
// to a point to be visible.
func (tp *StdoutTap) Lines() [][]byte {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	out := make([][]byte, len(tp.lines))
	copy(out, tp.lines)
	return out
}

// Restore puts os.Stdout back, rebinds the logger to it, and waits for the drain
// goroutine to finish so all emitted lines are captured. Idempotent.
func (tp *StdoutTap) Restore() {
	tp.mu.Lock()
	if tp.stopped {
		tp.mu.Unlock()
		return
	}
	tp.stopped = true
	tp.mu.Unlock()

	// Closing the write end flushes EOF to the scanner so the drain goroutine
	// exits; then restore os.Stdout and rebind the logger to it.
	_ = tp.w.Close()
	<-tp.done
	os.Stdout = tp.orig
	log.SetLevel(log.LevelInfo) // rebind logger to the restored stdout
	_ = tp.r.Close()
}

// TB is the subset of *testing.T the assertions use (mirrors contractprobe.TB).
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}
