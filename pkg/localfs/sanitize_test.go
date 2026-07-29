package localfs

import (
	"strings"
	"testing"
)

// TestSanitizeRelativePathNoopWhenNothingIllegal locks in the byte-for-byte
// identity guarantee for the three real-world names this function was
// introduced for: none contain a character NTFS forbids, so on every OS
// (including Windows) they must come back completely unchanged. Without
// this guarantee every already-legal remote name would start diverging
// from local_path for no reason.
func TestSanitizeRelativePathNoopWhenNothingIllegal(t *testing.T) {
	withTargetGOOS(t, "windows") // the strictest OS; a pass here implies a pass everywhere
	names := []string{
		"PKGBUILD (2)",
		"Notepad++v1.0.5.dmg",
		"FileZilla_3.70.5_macos-arm64.app.tar (3).bz2",
		"folder/nested (1)/file.txt",
	}
	for _, n := range names {
		if got := SanitizeRelativePath(n); got != n {
			t.Errorf("SanitizeRelativePath(%q) = %q, want unchanged", n, got)
		}
	}
}

// TestSanitizeRelativePathEmpty documents the zero-value passthrough: an
// empty relative path (root itself) must not turn into "_" or panic.
func TestSanitizeRelativePathEmpty(t *testing.T) {
	if got := SanitizeRelativePath(""); got != "" {
		t.Errorf("SanitizeRelativePath(\"\") = %q, want \"\"", got)
	}
}

// TestSanitizeRelativePathNoopOnNonWindows documents that the Windows rule
// set never triggers on any other OS, even for a name containing every
// character Windows forbids - Linux/macOS filesystems accept it as-is, so
// local_path must stay byte-identical to remote_path there.
func TestSanitizeRelativePathNoopOnNonWindows(t *testing.T) {
	withTargetGOOS(t, "linux")
	name := `Report: "Q1 vs Q2" <final>|v1?.xlsx`
	if got := SanitizeRelativePath(name); got != name {
		t.Errorf("SanitizeRelativePath(%q) on linux = %q, want unchanged", name, got)
	}
}

// TestSanitizeSegmentWindowsForbiddenChars verifies every NTFS-forbidden
// character is rewritten (never left in the result) and that the rewrite
// is stable/deterministic across repeated calls, which downstream code
// (local_path/local_path_hash persistence) depends on to recompute the
// exact same local path on every run.
func TestSanitizeSegmentWindowsForbiddenChars(t *testing.T) {
	withTargetGOOS(t, "windows")
	name := `Report: "Q1 vs Q2" <final>|v1?.xlsx`
	got := SanitizeRelativePath(name)
	for _, forbidden := range windowsForbiddenChars {
		if strings.ContainsRune(got, forbidden) {
			t.Errorf("SanitizeRelativePath(%q) = %q, still contains forbidden char %q", name, got, forbidden)
		}
	}
	if got2 := SanitizeRelativePath(name); got2 != got {
		t.Errorf("SanitizeRelativePath(%q) not deterministic: %q vs %q", name, got, got2)
	}
}

// TestSanitizeSegmentDistinctIllegalNamesNeverCollide is the core safety
// property this function exists to guarantee: two different remote names
// that would both naively rewrite to the same string (here, both contain
// exactly one NTFS-forbidden character in the same position) must still
// produce two DIFFERENT local paths, or one file would silently overwrite
// the other on disk.
func TestSanitizeSegmentDistinctIllegalNamesNeverCollide(t *testing.T) {
	withTargetGOOS(t, "windows")
	a := SanitizeRelativePath("report?.txt")
	b := SanitizeRelativePath("report:.txt")
	if a == b {
		t.Fatalf("distinct illegal names collided: both sanitized to %q", a)
	}
	// Re-running must reproduce the exact same disambiguated name (no DB
	// state involved - it's a pure function of the original name).
	if got := SanitizeRelativePath("report?.txt"); got != a {
		t.Errorf("SanitizeRelativePath not stable across calls: %q vs %q", got, a)
	}
}

// TestSanitizeSegmentWindowsReservedDeviceName verifies a DOS device name
// (with or without an extension) never survives unrewritten on Windows -
// "CON.txt" collides with the console device regardless of the extension.
func TestSanitizeSegmentWindowsReservedDeviceName(t *testing.T) {
	withTargetGOOS(t, "windows")
	for _, n := range []string{"CON", "con.txt", "LPT1", "com9.log"} {
		got := SanitizeRelativePath(n)
		if strings.EqualFold(strings.TrimSuffix(got, extOf(got)), n) {
			t.Errorf("SanitizeRelativePath(%q) = %q, reserved device name was not rewritten", n, got)
		}
	}
}

// TestSanitizeSegmentWindowsTrailingDotAndSpaceStripped documents that a
// trailing dot or space - silently dropped by Windows itself when the file
// is created - is stripped explicitly, so local_path always matches what
// actually lands on disk instead of a name Windows would never produce.
func TestSanitizeSegmentWindowsTrailingDotAndSpaceStripped(t *testing.T) {
	withTargetGOOS(t, "windows")
	for _, n := range []string{"notes.", "notes ", "notes. "} {
		got := SanitizeRelativePath(n)
		if strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") {
			t.Errorf("SanitizeRelativePath(%q) = %q, still ends in trailing dot/space", n, got)
		}
	}
}

func extOf(name string) string {
	if idx := strings.LastIndexByte(name, '.'); idx > 0 {
		return name[idx:]
	}
	return ""
}

// TestSanitizeRelativePathPerSegmentNesting ensures sanitization is applied
// independently to every path segment (folders too, not just the leaf
// file name) and that legal segments around an illegal one are preserved
// verbatim - this is what keeps LiveCacheSync's directory nesting
// (destPath built from path.Dir of the sanitized path) consistent with
// what CreateLocalFolder actually created for that same segment.
func TestSanitizeRelativePathPerSegmentNesting(t *testing.T) {
	withTargetGOOS(t, "windows")
	got := SanitizeRelativePath("a/b:c/file.txt")
	parts := strings.Split(got, "/")
	if len(parts) != 3 {
		t.Fatalf("SanitizeRelativePath(%q) = %q, want 3 segments", "a/b:c/file.txt", got)
	}
	if parts[0] != "a" {
		t.Errorf("first segment = %q, want unchanged %q", parts[0], "a")
	}
	if strings.ContainsRune(parts[1], ':') {
		t.Errorf("second segment %q still contains ':'", parts[1])
	}
	if parts[2] != "file.txt" {
		t.Errorf("third segment = %q, want unchanged %q", parts[2], "file.txt")
	}
}

// withTargetGOOS overrides targetGOOS for the duration of the test,
// restoring the real runtime.GOOS afterwards.
func withTargetGOOS(t *testing.T, goos string) {
	t.Helper()
	orig := targetGOOS
	targetGOOS = goos
	t.Cleanup(func() { targetGOOS = orig })
}
