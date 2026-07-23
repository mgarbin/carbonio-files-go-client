// Package appdir resolves the single per-user directory this application
// uses for all of its on-disk state: log files and SQLite databases (both
// the desktop GUI's credential store and the CLI's token/cache store).
//
// The directory is the same on every supported OS: a hidden folder named
// ".carbonio_files_sync" directly under the user's home directory -
// "$HOME/.carbonio_files_sync" on Linux and macOS,
// "%USERPROFILE%\.carbonio_files_sync" on Windows (os.UserHomeDir resolves
// the right environment variable for each). This is a deliberate departure
// from each OS' usual convention (XDG dirs on Linux, AppData on Windows,
// ~/Library on macOS): a single well-known location keeps logs and
// databases easy to find and back up regardless of platform.
package appdir

import (
	"os"
	"path/filepath"
)

// DirName is the hidden folder name created under the user's home directory.
const DirName = ".carbonio_files_sync"

// Dir returns "$HOME/.carbonio_files_sync", creating it (mode 0700) if it
// does not exist yet. If the home directory cannot be resolved, it falls
// back to DirName relative to the current working directory rather than
// failing outright.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, DirName)
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// Path joins Dir() with name, e.g. appdir.Path("gui-config.db").
func Path(name string) string {
	return filepath.Join(Dir(), name)
}
