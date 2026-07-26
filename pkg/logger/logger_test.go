package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TestConfigNormalizeDefaults locks in normalize()'s zero-value behavior:
// every empty field must fall back to its documented default (info level,
// console format, console output, DefaultPath), and already-valid fields
// must be left untouched.
func TestConfigNormalizeDefaults(t *testing.T) {
	got := Config{}.normalize()

	if got.Level != zerolog.InfoLevel.String() {
		t.Errorf("Level = %q, want %q", got.Level, zerolog.InfoLevel.String())
	}
	if got.Format != FormatConsole {
		t.Errorf("Format = %q, want %q", got.Format, FormatConsole)
	}
	if got.Output != OutputConsole {
		t.Errorf("Output = %q, want %q", got.Output, OutputConsole)
	}
	if got.FilePath != DefaultPath {
		t.Errorf("FilePath = %q, want %q", got.FilePath, DefaultPath)
	}

	valid := Config{
		Level:    "debug",
		Format:   FormatJSON,
		Output:   OutputBoth,
		FilePath: "/tmp/custom.log",
	}
	normalized := valid.normalize()
	if normalized != valid {
		t.Errorf("normalize() on already-valid config = %+v, want unchanged %+v", normalized, valid)
	}
}

// TestConfigNormalizeInvalidEnums checks that normalize() degrades
// gracefully on typo'd/unknown enum values rather than preserving them:
// an unrecognized Format or Output must fall back to its default instead
// of passing through as-is.
func TestConfigNormalizeInvalidEnums(t *testing.T) {
	got := Config{Format: "xml", Output: "carrier-pigeon"}.normalize()

	if got.Format != FormatConsole {
		t.Errorf("Format = %q, want fallback %q", got.Format, FormatConsole)
	}
	if got.Output != OutputConsole {
		t.Errorf("Output = %q, want fallback %q", got.Output, OutputConsole)
	}

	// Level has no enum validation in normalize (only emptiness), so an
	// invalid Level string must be preserved unchanged here - it's Init's
	// job (via zerolog.ParseLevel) to reject it later.
	levelGot := Config{Level: "not-a-level"}.normalize()
	if levelGot.Level != "not-a-level" {
		t.Errorf("Level = %q, want preserved %q", levelGot.Level, "not-a-level")
	}
}

// TestDefault checks Default() returns the exact documented baseline
// config (info/console/console, empty FilePath) and nothing else, since
// callers rely on it as the zero-config starting point.
func TestDefault(t *testing.T) {
	want := Config{
		Level:  "info",
		Format: FormatConsole,
		Output: OutputConsole,
	}
	if got := Default(); got != want {
		t.Fatalf("Default() = %+v, want %+v", got, want)
	}
}

// TestInitFileOutputCreatesFileAndWrites verifies that Init with
// Output: OutputFile actually opens (and creates, including missing
// parent directories) the configured file, that logging through the
// global logger afterward writes real bytes containing the message to
// that file, and that the returned closer is non-nil and closes
// cleanly.
func TestInitFileOutputCreatesFileAndWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", "test.log")

	closer, err := Init(Config{
		Level:    "info",
		Format:   FormatJSON,
		Output:   OutputFile,
		FilePath: path,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if closer == nil {
		t.Fatal("Init() returned nil closer")
	}
	t.Cleanup(func() {
		if cerr := closer.Close(); cerr != nil {
			t.Errorf("closer.Close() error = %v", cerr)
		}
	})

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("log file was not created: %v", statErr)
	}

	const marker = "hello-from-logger-test"
	log.Logger.Info().Msg(marker)

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("failed to read log file: %v", readErr)
	}
	if len(data) == 0 {
		t.Fatal("log file is empty after writing a log line")
	}
	if !strings.Contains(string(data), marker) {
		t.Fatalf("log file contents = %q, want it to contain %q", string(data), marker)
	}
}

// TestInitConsoleOutputNoopCloser verifies that Init with
// Output: OutputConsole never opens a file: the returned closer must be
// non-nil (so callers can unconditionally defer Close()) but Close()
// must be a no-op that never errors.
func TestInitConsoleOutputNoopCloser(t *testing.T) {
	closer, err := Init(Config{
		Level:  "warn",
		Format: FormatConsole,
		Output: OutputConsole,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if closer == nil {
		t.Fatal("Init() returned nil closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() error = %v, want nil no-op close", err)
	}
	// Closing twice must still be safe/no-op: proves it never touched a file.
	if err := closer.Close(); err != nil {
		t.Fatalf("second closer.Close() error = %v, want nil no-op close", err)
	}
}

// TestInitInvalidLevel checks that Init rejects an unparsable Level
// string with a non-nil error instead of silently falling back, since
// (unlike Format/Output) Level validation happens via
// zerolog.ParseLevel, not normalize().
func TestInitInvalidLevel(t *testing.T) {
	closer, err := Init(Config{Level: "not-a-real-level", Output: OutputConsole})
	if err == nil {
		t.Fatal("Init() error = nil, want non-nil for invalid level")
	}
	if closer != nil {
		t.Fatalf("Init() closer = %v, want nil on error", closer)
	}
}
