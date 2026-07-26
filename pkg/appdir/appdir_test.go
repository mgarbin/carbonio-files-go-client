package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirCreatesUnderHome verifies Dir() resolves to
// "$HOME/.carbonio_files_sync" (via HOME, overridden with t.Setenv) and that
// it actually creates the directory on disk with mode 0700, since callers
// (log files, SQLite stores) rely on both the path and its existence.
func TestDirCreatesUnderHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	want := filepath.Join(tmp, DirName)
	got := Dir()
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Dir() did not create %q on disk: %v", got, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q exists but is not a directory", got)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("%q has mode %o, want 0700", got, perm)
	}
}

// TestDirIdempotent locks in that calling Dir() repeatedly on an
// already-existing directory is safe: MkdirAll on an existing directory must
// not fail or otherwise change the returned path.
func TestDirIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	first := Dir()
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first Dir() call did not create %q: %v", first, err)
	}

	second := Dir()
	if second != first {
		t.Fatalf("second Dir() = %q, want %q (same as first call)", second, first)
	}
	info, err := os.Stat(second)
	if err != nil {
		t.Fatalf("directory %q no longer exists after second Dir() call: %v", second, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q exists but is not a directory", second)
	}
}

// TestPathJoinsDir ensures Path(name) is exactly filepath.Join(Dir(), name),
// so callers get a portable, correctly-separated path to files (e.g. SQLite
// databases) inside the app directory.
func TestPathJoinsDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	want := filepath.Join(Dir(), "foo.db")
	got := Path("foo.db")
	if got != want {
		t.Fatalf("Path(%q) = %q, want %q", "foo.db", got, want)
	}
}
