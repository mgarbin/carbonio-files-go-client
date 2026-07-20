//go:build darwin

package i18n

import (
	"os"
	"os/exec"
	"strings"
)

// DetectOSLocale returns the raw OS locale hint on macOS. GUI apps launched
// from Finder/Launchpad don't inherit a shell environment, so the POSIX
// locale variables are checked first and "defaults read -g AppleLocale" (the
// system UI language) is used as the fallback.
func DetectOSLocale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
