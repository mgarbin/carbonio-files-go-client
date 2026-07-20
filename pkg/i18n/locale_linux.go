//go:build linux

package i18n

import "os"

// DetectOSLocale returns the raw OS locale hint on Linux, read from the
// standard POSIX locale environment variables in glibc's priority order.
func DetectOSLocale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
