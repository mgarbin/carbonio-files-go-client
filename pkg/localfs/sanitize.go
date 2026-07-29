package localfs

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
)

// windowsReservedStems are DOS/Windows device names that collide with a
// file regardless of extension or case ("con.txt" is still the console
// device on Windows, not a normal file).
var windowsReservedStems = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// targetGOOS defaults to runtime.GOOS but is a var (not a direct
// runtime.GOOS reference) so tests can override it to exercise the
// Windows-specific rewrite rules on any host OS, since the whole point of
// this file is behavior that only matters when the sync client actually
// runs on Windows.
var targetGOOS = runtime.GOOS

// windowsForbiddenChars are the characters NTFS/Windows reject in a path
// component, beyond the C0 control range (checked separately).
const windowsForbiddenChars = `<>:"\|?*`

// SanitizeRelativePath makes every '/'-separated segment of relativePath
// safe to create on the CURRENT OS's filesystem, so it is safe to pass to
// filepath.Join/os.MkdirAll/os.Create as-is.
//
// It is the single place local_path must be derived from remote_path: the
// remote node name/extension the server hands back can legally contain
// characters (':', '?', '"', trailing dots/spaces, "CON"...) that are
// perfectly valid on the Linux/macOS filesystem the server itself likely
// runs on, but rejected outright by NTFS. Without this step, DownloadFile's
// bare os.Create(destPath+"/"+fileName) fails on Windows for any such
// remote name, and any code that (wrongly) reused remote_path verbatim as
// local_path would point at a path that was never actually created on
// disk.
//
// Segments that are already legal on the current OS come back byte-for-
// byte unchanged - sanitization only ever kicks in on Windows, and only
// for the specific characters/names Windows forbids, so the overwhelming
// common case (Linux/macOS, or a Windows-legal name) is a no-op and
// local_path keeps matching remote_path exactly, as every other part of
// the sync engine (LiveSyncCheck, the untracked-path union in
// updateCacheSync) already assumes.
//
// When a segment genuinely needs rewriting, a deterministic 6-hex-char
// suffix derived from sha256 of the ORIGINAL segment is appended before
// its extension. This is a pure function of remote_path alone - no
// database state is required to keep it stable across runs - and it
// guarantees two different illegal names can never collapse onto the same
// sanitized path (e.g. "report?.txt" and "report:.txt" would otherwise
// both become "report_.txt").
func SanitizeRelativePath(relativePath string) string {
	if relativePath == "" {
		return relativePath
	}
	segments := strings.Split(relativePath, "/")
	for i, seg := range segments {
		segments[i] = sanitizeSegment(seg)
	}
	return strings.Join(segments, "/")
}

func sanitizeSegment(name string) string {
	// "." and ".." are path syntax, not real names; NormalizeRelativePath/
	// filepath.Clean already strip these out of anything reaching here in
	// practice, but leave them alone defensively rather than mangle them.
	if name == "" || name == "." || name == ".." {
		return name
	}

	cleaned := name

	// Universal, regardless of OS: a single path segment can never
	// legitimately contain the separator this package uses to join
	// segments, or a NUL byte (illegal in a path on every OS Go supports).
	cleaned = strings.Map(func(r rune) rune {
		if r == '/' || r == 0 {
			return '_'
		}
		return r
	}, cleaned)

	if targetGOOS == "windows" {
		cleaned = strings.Map(func(r rune) rune {
			if r < 0x20 || strings.ContainsRune(windowsForbiddenChars, r) {
				return '_'
			}
			return r
		}, cleaned)

		trimmed := strings.TrimRight(cleaned, " .")
		if trimmed == "" {
			trimmed = "_"
		}
		cleaned = trimmed

		stem := cleaned
		if idx := strings.IndexByte(cleaned, '.'); idx >= 0 {
			stem = cleaned[:idx]
		}
		if windowsReservedStems[strings.ToUpper(stem)] {
			cleaned = "_" + cleaned
		}
	}

	if cleaned == name {
		return name
	}

	// The segment needed rewriting: disambiguate deterministically from
	// the ORIGINAL name so distinct illegal names never collide, without
	// needing any persisted state to reproduce the same result later.
	ext := ""
	base := cleaned
	if idx := strings.LastIndexByte(cleaned, '.'); idx > 0 {
		ext = cleaned[idx:]
		base = cleaned[:idx]
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:])[:6]
	return base + "_" + suffix + ext
}
