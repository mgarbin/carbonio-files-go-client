// Package img embeds the image assets shipped with the desktop client.
package img

import _ "embed"

// Icon is the PNG (512x512, RGBA) tray icon shown in the system tray /
// notification area when the desktop GUI is minimized, and — via
// options.Linux.Icon in wails.Run (see runGUI in
// cmd/carbonio-files-go-client/main.go) — the Linux window/taskbar icon.
// Keep it in this package so img/ico.png remains the single source of
// truth for the asset.
//
// The Windows .exe icon (and, by extension, the window/taskbar icon
// there, which Wails reads from the same compiled-in resource) is a
// separate asset: img/icon.ico, a multi-resolution ICO rendered from
// this same PNG and linked in as a Windows resource via
// cmd/carbonio-files-go-client/rsrc_windows_amd64.syso. Run
// img/regenerate-windows-icon.sh to regenerate both after changing
// img/ico.png; macOS has no titlebar icon and its Dock icon needs an
// .app bundle, which this project's raw-binary builds don't produce.
//
//go:embed ico.png
var Icon []byte
