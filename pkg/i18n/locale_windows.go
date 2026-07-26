//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

// DetectOSLocale returns the raw OS locale hint on Windows via the
// kernel32!GetUserDefaultLocaleName API (e.g. "it-IT", "en-US").
func DetectOSLocale() string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getUserDefaultLocaleName := kernel32.NewProc("GetUserDefaultLocaleName")

	// LOCALE_NAME_MAX_LENGTH is 85.
	buf := make([]uint16, 85)
	ret, _, _ := getUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
