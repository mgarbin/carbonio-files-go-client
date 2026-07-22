// Package logger configures the process-wide zerolog logger used by every
// package in carbonio-files-go-client. It is the single place that knows how
// to turn a Config (sourced from CLI flags, config.yaml, or the SQLite
// config table) into a concrete zerolog writer setup.
package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Format selects how each log line is encoded.
type Format string

const (
	// FormatConsole renders human-readable, colorized lines (zerolog's
	// ConsoleWriter). This is the default: best suited for interactive use.
	FormatConsole Format = "console"
	// FormatJSON renders one JSON object per line, suited for log
	// aggregators and machine parsing.
	FormatJSON Format = "json"
)

// Output selects where log lines are written to.
type Output string

const (
	// OutputConsole writes to stderr only.
	OutputConsole Output = "console"
	// OutputFile writes to the configured file only.
	OutputFile Output = "file"
	// OutputBoth writes to both stderr and the configured file.
	OutputBoth Output = "both"
)

// DefaultPath is the log file path used when Config.FilePath is empty and a
// file writer is required (Output file/both).
const DefaultPath = "logs/carbonio-files-go-client.log"

// Config drives Init. Zero-value fields fall back to sane defaults, so a
// zero Config produces console/info logging - the same output the binary
// had by default before zerolog was introduced.
type Config struct {
	// Level is a zerolog level name: trace, debug, info, warn, error,
	// fatal, panic, disabled. Empty/invalid falls back to "info".
	Level string
	// Format is FormatConsole or FormatJSON. Empty falls back to FormatConsole.
	Format Format
	// Output is OutputConsole, OutputFile or OutputBoth. Empty falls back
	// to OutputConsole.
	Output Output
	// FilePath is where log lines land when Output is file/both. Empty
	// falls back to DefaultPath. Parent directories are created as needed.
	FilePath string
}

// Default returns the built-in default configuration: info level, console
// format, console output.
func Default() Config {
	return Config{
		Level:  zerolog.InfoLevel.String(),
		Format: FormatConsole,
		Output: OutputConsole,
	}
}

// normalize fills every empty field with its default and validates enums,
// falling back to the default value rather than failing on an unknown one -
// a typo'd config field should degrade gracefully, not crash the app.
func (c Config) normalize() Config {
	if c.Level == "" {
		c.Level = zerolog.InfoLevel.String()
	}
	switch c.Format {
	case FormatConsole, FormatJSON:
	default:
		c.Format = FormatConsole
	}
	switch c.Output {
	case OutputConsole, OutputFile, OutputBoth:
	default:
		c.Output = OutputConsole
	}
	if c.FilePath == "" {
		c.FilePath = DefaultPath
	}
	return c
}

// Init configures the global zerolog logger (github.com/rs/zerolog/log.Logger)
// according to cfg and returns an io.Closer for the underlying log file, if
// one was opened. Callers should defer closer.Close() (closer is always
// non-nil; it is a no-op when no file was opened).
func Init(cfg Config) (io.Closer, error) {
	cfg = cfg.normalize()

	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}

	var file *os.File
	var writers []io.Writer

	if cfg.Output == OutputConsole || cfg.Output == OutputBoth {
		writers = append(writers, encode(os.Stderr, cfg.Format, false))
	}

	if cfg.Output == OutputFile || cfg.Output == OutputBoth {
		file, err = openLogFile(cfg.FilePath)
		if err != nil {
			return nil, err
		}
		// Never colorize file output, regardless of terminal detection.
		writers = append(writers, encode(file, cfg.Format, true))
	}

	var w io.Writer
	switch len(writers) {
	case 1:
		w = writers[0]
	default:
		w = zerolog.MultiLevelWriter(writers...)
	}

	zerolog.SetGlobalLevel(level)
	log.Logger = zerolog.New(w).With().Timestamp().Logger()

	return closerFunc(func() error {
		if file == nil {
			return nil
		}
		return file.Close()
	}), nil
}

// encode wraps w with a ConsoleWriter when format is console; JSON needs no
// wrapping since zerolog writes JSON natively.
func encode(w io.Writer, format Format, noColor bool) io.Writer {
	if format == FormatJSON {
		return w
	}
	return zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339, NoColor: noColor}
}

// openLogFile opens path for appending, creating parent directories and the
// file itself as needed.
func openLogFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("cannot create log directory %q: %w", dir, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open log file %q: %w", path, err)
	}
	return f, nil
}

// closerFunc adapts a func() error to io.Closer.
type closerFunc func() error

func (f closerFunc) Close() error { return f() }
