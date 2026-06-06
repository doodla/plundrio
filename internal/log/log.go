package log

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var log zerolog.Logger

// hubWriter is the second leg of the log fan-out: every event that passes the
// global level gate is written here as well as to the colored stdout console.
// It is a package-level indirection so the dashboard can install a real sink
// (the SSE hub) once, after which every reconfigure of the logger keeps feeding
// it. Until SetHubWriter installs one it is io.Discard, so non-dashboard builds
// and the pre-registration window cost nothing.
//
// The indirection (a swappable *io.Writer guarded by a mutex, fronted by
// hubLeg) matters because configureLogger runs more than once: at init, on
// every SetLevel, and the logassert test harness swaps os.Stdout and re-runs
// SetLevel. A writer captured by value into the MultiLevelWriter at configure
// time would be frozen to whatever sink existed then; routing through hubLeg
// means a SetHubWriter before OR after a reconfigure both take effect.
var (
	hubMu  sync.RWMutex
	hubDst io.Writer = io.Discard
)

// hubLeg is the stable io.Writer handed to zerolog's MultiLevelWriter. It
// forwards to whatever hubDst currently is. It MUST NEVER block or error from
// zerolog's perspective: a slow SSE subscriber must degrade only its own
// stream, never back-pressure the logger (a logger an HTTP client can stall is
// the bug to avoid). The installed sink (the hub) is itself responsible for
// non-blocking fan-out; hubLeg additionally guarantees the (len(p), nil) return
// contract regardless of what the sink reports.
type hubLeg struct{}

func (hubLeg) Write(p []byte) (int, error) {
	hubMu.RLock()
	dst := hubDst
	hubMu.RUnlock()
	// Ignore the sink's return values: zerolog must always see a successful,
	// complete write so a misbehaving sink can never stall or error the log
	// path. The sink contract (dashboard hub) is non-blocking by construction.
	_, _ = dst.Write(p)
	return len(p), nil
}

// SetHubWriter installs the sink the hub leg forwards to. The dashboard calls
// this once from dashboard.New with the SSE hub's writer. Passing nil resets to
// io.Discard (used on shutdown / in tests). The sink must be non-blocking; see
// hubLeg.
func SetHubWriter(w io.Writer) {
	hubMu.Lock()
	if w == nil {
		hubDst = io.Discard
	} else {
		hubDst = w
	}
	hubMu.Unlock()
}

// LogLevel represents the logging level
type LogLevel string

const (
	// Log levels
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
	LevelNone  LogLevel = "none"
)

func init() {
	// Default to info level
	level := getLogLevel()

	// Configure zerolog
	zerolog.TimeFieldFormat = time.RFC3339

	// Initial logger setup
	configureLogger(level)
}

// configureLogger sets up the logger with the specified level.
//
// The logger fans out to two legs via zerolog.MultiLevelWriter:
//   - a ConsoleWriter formatting colored output to stdout (read fresh each call
//     so the logassert harness's os.Stdout swap is honored), and
//   - hubLeg, the stable forwarder to the dashboard SSE hub.
//
// zerolog gates by the global level BEFORE invoking any writer, so the hub leg
// only ever receives events at or above the active level — a live log_level
// change widens/narrows the SSE log stream for free.
func configureLogger(level LogLevel) {
	// Configure console output writer with colors enabled by default. Reading
	// os.Stdout here (not caching it) is load-bearing for the logassert tap,
	// which swaps os.Stdout then calls SetLevel to rebind.
	console := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		NoColor:    false, // Always use colors
	}

	mw := zerolog.MultiLevelWriter(console, hubLeg{})
	log = zerolog.New(mw).With().Timestamp().Logger()

	// Set log level
	setLogLevel(level)
}

// getLogLevel determines the log level from environment
func getLogLevel() LogLevel {
	if envLevel := os.Getenv("PLDR_LOG_LEVEL"); envLevel != "" {
		return LogLevel(strings.ToLower(envLevel))
	}
	return LevelInfo
}

// setLogLevel sets the zerolog level
func setLogLevel(level LogLevel) {
	switch level {
	case LevelDebug:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case LevelInfo:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case LevelWarn:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case LevelError:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case LevelFatal:
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case LevelNone:
		zerolog.SetGlobalLevel(zerolog.Disabled)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// SetLevel sets the global log level
func SetLevel(level LogLevel) {
	// Reconfigure the logger
	configureLogger(level)
}

// GetLevel returns the active global log level as a LogLevel. It reads zerolog's
// global level (the gate every log call passes through), so it reflects the live
// state after any SetLevel — including a live dashboard log_level change. This
// is the honest source for the settings API's reported log_level value.
func GetLevel() LogLevel {
	switch zerolog.GlobalLevel() {
	case zerolog.DebugLevel:
		return LevelDebug
	case zerolog.InfoLevel:
		return LevelInfo
	case zerolog.WarnLevel:
		return LevelWarn
	case zerolog.ErrorLevel:
		return LevelError
	case zerolog.FatalLevel:
		return LevelFatal
	case zerolog.Disabled:
		return LevelNone
	default:
		return LevelInfo
	}
}

// Debug returns a new Debug level event logger with component context
func Debug(component string) *zerolog.Event {
	return log.Debug().Str("component", component)
}

// Info returns a new Info level event logger with component context
func Info(component string) *zerolog.Event {
	return log.Info().Str("component", component)
}

// Warn returns a new Warn level event logger with component context
func Warn(component string) *zerolog.Event {
	return log.Warn().Str("component", component)
}

// Error returns a new Error level event logger with component context
func Error(component string) *zerolog.Event {
	return log.Error().Str("component", component)
}

// Fatal returns a new Fatal level event logger with component context
func Fatal(component string) *zerolog.Event {
	return log.Fatal().Str("component", component)
}
