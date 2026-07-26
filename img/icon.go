// Package img embeds the image assets shipped with the desktop client.
package img

import _ "embed"

// Icon is the PNG (512x512, RGBA) tray icon shown in the system tray /
// notification area when the desktop GUI is minimized. Keep it in this
// package so img/ico.png remains the single source of truth for the asset.
//
//go:embed ico.png
var Icon []byte
