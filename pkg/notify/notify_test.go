package notify

import (
	"testing"

	"github.com/gen2brain/beeep"
)

// TestInitSetsAppName locks in the init() side effect: beeep.AppName must be
// set to "Carbonio Files Sync" so the OS notification center identifies the
// app correctly (e.g. in Windows'/Linux desktops' per-app notification
// settings) before any caller has a chance to override it.
func TestInitSetsAppName(t *testing.T) {
	want := "Carbonio Files Sync"
	if beeep.AppName != want {
		t.Fatalf("beeep.AppName = %q, want %q", beeep.AppName, want)
	}
}

// TestSyncChangeDoesNotPanic verifies SyncChange never propagates an error
// or panics even when the underlying OS notification backend is unavailable
// (as is the case in headless/CI environments). Failures are only logged
// internally - see the doc comment on SyncChange - so the only externally
// observable contract is that the call returns normally.
func TestSyncChangeDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SyncChange panicked: %v", r)
		}
	}()

	SyncChange("New file synced", "report.pdf was added to the synced folder")
}

// TestSyncChangeEmptyStrings exercises SyncChange with empty title/body to
// confirm it tolerates degenerate input without panicking, mirroring how a
// caller might invoke it with an unlocalized/blank message.
func TestSyncChangeEmptyStrings(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SyncChange panicked on empty input: %v", r)
		}
	}()

	SyncChange("", "")
}
