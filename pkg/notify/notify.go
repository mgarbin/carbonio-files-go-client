// Package notify sends native desktop notifications from the GUI app (see
// cmd/carbonio-files-go-client) about sync activity. It wraps
// github.com/gen2brain/beeep, which drives the host OS's notification
// center - D-Bus/notify-send on Linux, terminal-notifier/osascript on
// macOS, the Toast/PowerShell APIs on Windows - behind one function so the
// rest of the codebase, in particular the CLI entry point and pkg/actions,
// never needs to depend on it directly.
package notify

import (
	"carbonio-files-go-client/img"

	"github.com/gen2brain/beeep"
	"github.com/rs/zerolog/log"
)

func init() {
	// Identifies the app to the OS notification center (e.g. shown in
	// Windows' and Linux desktops' per-app notification settings).
	// Overwritten with the localized app name by SyncChange's callers if
	// desired - see App.notifySyncSummary.
	beeep.AppName = "Carbonio Files Sync"
}

// SyncChange sends a desktop notification reporting new, modified, or
// deleted synced documents. title/body are expected to already be
// localized by the caller (see pkg/i18n). A failure to display the
// notification (e.g. no notification daemon running on a headless Linux
// box) is logged and otherwise ignored - it must never interrupt sync.
func SyncChange(title, body string) {
	if err := beeep.Notify(title, body, img.Icon); err != nil {
		log.Warn().Err(err).Msg("Sending desktop notification failed")
	}
}
