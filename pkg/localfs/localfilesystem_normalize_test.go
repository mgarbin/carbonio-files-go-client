package localfs

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// TestSha384Base64MatchesIndependentComputation locks in the exact encoding
// contract of Sha384Base64: SHA-384 of the file content, standard base64,
// then every '/' rewritten to ',' (so the digest is safe to embed in
// contexts, e.g. HTTP headers or filenames, that treat '/' specially).
// The expected value is computed here independently of the production
// code path (separate hasher, separate encode+replace) so the test can't
// pass merely by mirroring a shared bug.
func TestSha384Base64MatchesIndependentComputation(t *testing.T) {
	dir := t.TempDir()
	content := []byte("the quick brown fox jumps over the lazy dog, repeated for good measure")
	filePath := filepath.Join(dir, "content.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	hasher := sha512.New384()
	if _, err := hasher.Write(content); err != nil {
		t.Fatalf("hasher.Write failed: %v", err)
	}
	sum := hasher.Sum(nil)
	want := strings.ReplaceAll(base64.StdEncoding.EncodeToString(sum), "/", ",")

	got, err := Sha384Base64(filePath)
	if err != nil {
		t.Fatalf("Sha384Base64 returned error: %v", err)
	}
	if got != want {
		t.Errorf("Sha384Base64 = %q, want %q", got, want)
	}
	if strings.Contains(got, "/") {
		t.Errorf("Sha384Base64 result %q still contains '/', should have been replaced with ','", got)
	}
}

// TestSha384Base64NonexistentFile ensures a missing file surfaces the
// underlying os.Open error instead of a hash of empty content, so callers
// can distinguish "file vanished" from "empty file".
func TestSha384Base64NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.txt")

	got, err := Sha384Base64(missing)
	if err == nil {
		t.Fatalf("expected error for nonexistent file, got nil (result=%q)", got)
	}
	if got != "" {
		t.Errorf("expected empty result on error, got %q", got)
	}
}

// TestNormalizeRelativePathNestedUnderRoot covers the common case: a
// relative path pointing at a nested file inside root normalizes to a
// forward-slash-joined relative path equal to the input (already in the
// expected form), with the original input returned unchanged for
// UI/logging purposes.
func TestNormalizeRelativePathNestedUnderRoot(t *testing.T) {
	root := t.TempDir()
	input := "sub/dir/file.txt"

	relative, original, err := NormalizeRelativePath(root, input)
	if err != nil {
		t.Fatalf("NormalizeRelativePath returned error: %v", err)
	}
	if relative != "sub/dir/file.txt" {
		t.Errorf("relative = %q, want %q", relative, "sub/dir/file.txt")
	}
	if original != input {
		t.Errorf("original = %q, want unchanged input %q", original, input)
	}
}

// TestNormalizeRelativePathAbsoluteOutsideRootFallsBackToCleanAbsolute
// documents the exact fallback the source takes when filepath.Rel cannot
// relativize the path (here: a relative root combined with an absolute
// input outside it, which filepath.Rel itself rejects with an error
// because one side is absolute and the other is not). The source
// swallows that internal error and falls back to using the filepath.Clean'd
// absolute path as "relative", while its own err return value stays nil
// and original is returned exactly as given.
func TestNormalizeRelativePathAbsoluteOutsideRootFallsBackToCleanAbsolute(t *testing.T) {
	root := "relroot" // relative root: filepath.Rel(root, <absolute>) errors internally
	input := "/some/abs/path/../file.txt"
	wantAbsFallback := filepath.Clean(input) // "/some/abs/file.txt"

	relative, original, err := NormalizeRelativePath(root, input)
	if err != nil {
		t.Fatalf("NormalizeRelativePath's own err return must always be nil, got: %v", err)
	}
	if relative != wantAbsFallback {
		t.Errorf("relative = %q, want fallback clean-absolute %q", relative, wantAbsFallback)
	}
	if original != input {
		t.Errorf("original = %q, want unchanged input %q", original, input)
	}
}

// TestNormalizeRelativePathStripsDotPrefixAndTrailingSlash checks that a
// "./"-prefixed, trailing-slash-terminated relative input still comes out
// as a clean relative path with neither artifact, and that a path equal
// to root itself normalizes to the empty string (not "." or "./").
func TestNormalizeRelativePathStripsDotPrefixAndTrailingSlash(t *testing.T) {
	root := t.TempDir()

	relative, _, err := NormalizeRelativePath(root, "./sub/dir/")
	if err != nil {
		t.Fatalf("NormalizeRelativePath returned error: %v", err)
	}
	if relative != "sub/dir" {
		t.Errorf("relative = %q, want %q (no './' prefix, no trailing '/')", relative, "sub/dir")
	}
	if strings.HasPrefix(relative, "./") {
		t.Errorf("relative %q retains a './' prefix", relative)
	}
	if strings.HasSuffix(relative, "/") {
		t.Errorf("relative %q retains a trailing '/'", relative)
	}

	// Path equal to root: filepath.Rel(root, root) == ".", which the
	// source explicitly collapses to "".
	selfRelative, _, err := NormalizeRelativePath(root, ".")
	if err != nil {
		t.Fatalf("NormalizeRelativePath returned error: %v", err)
	}
	if selfRelative != "" {
		t.Errorf("relative for root itself = %q, want empty string", selfRelative)
	}
}

// TestNormalizeRelativePathNormalizesUnicodeNFDToNFC verifies the
// documented Unicode behavior: an NFD-decomposed input (base letter plus
// combining accent, produced by norm.NFD.String) is re-normalized to NFC
// (precomposed accented letter) in the returned relative path, even
// though the raw decomposed bytes were what the filesystem/caller
// supplied. Without this, two paths that a human reads as identical text
// but that differ in decomposition would hash / compare as different
// paths (see PathHash and sqlite path-keyed lookups).
func TestNormalizeRelativePathNormalizesUnicodeNFDToNFC(t *testing.T) {
	root := t.TempDir()
	composedNFC := "caf\u00e9" // precomposed 'é' (U+00E9)
	decomposedNFD := norm.NFD.String(composedNFC)
	if decomposedNFD == composedNFC {
		t.Fatalf("test setup invalid: NFD.String did not change %q", composedNFC)
	}

	relative, original, err := NormalizeRelativePath(root, decomposedNFD)
	if err != nil {
		t.Fatalf("NormalizeRelativePath returned error: %v", err)
	}
	if relative != composedNFC {
		t.Errorf("relative = %q (bytes %x), want NFC form %q (bytes %x)", relative, relative, composedNFC, composedNFC)
	}
	if relative == decomposedNFD {
		t.Errorf("relative %q was not renormalized away from the raw NFD input", relative)
	}
	if original != decomposedNFD {
		t.Errorf("original = %q, want unchanged raw NFD input %q", original, decomposedNFD)
	}
}

// TestPathHashDeterministicAndDistinct locks in PathHash's contract as a
// pure, deterministic function of its input: identical inputs always
// produce identical hashes, different inputs (almost always) produce
// different hashes, and the result is exactly hex(sha256(input)) as
// computed independently here — this is the fixed-length index key used
// by sqlite lookups, so any drift here would silently break cache joins.
func TestPathHashDeterministicAndDistinct(t *testing.T) {
	a := "sub/dir/file.txt"
	b := "sub/dir/other.txt"

	gotA1 := PathHash(a)
	gotA2 := PathHash(a)
	if gotA1 != gotA2 {
		t.Errorf("PathHash(%q) not deterministic: %q != %q", a, gotA1, gotA2)
	}

	gotB := PathHash(b)
	if gotA1 == gotB {
		t.Errorf("PathHash produced identical output %q for distinct inputs %q and %q", gotA1, a, b)
	}

	sumA := sha256.Sum256([]byte(a))
	wantA := hex.EncodeToString(sumA[:])
	if gotA1 != wantA {
		t.Errorf("PathHash(%q) = %q, want %q", a, gotA1, wantA)
	}
}

// TestComparePathMapsMulti exercises the documented diff semantics for
// every category the sync engine relies on: local-only, remote-only,
// both-present-and-identical (no diff emitted), and both-present-but-
// differing along each individually-checked field (digest, size+mtime
// together, mtime alone without a size change, delete timestamp, and
// file/folder type mismatch). The size+mtime and mtime-alone cases in
// particular pin down the source's actual (non-obvious) semantics: a
// ModifyTimeDifferent diff is only emitted when the size ALSO differs.
func TestComparePathMapsMulti(t *testing.T) {
	local := map[string]ItemInfo{
		"onlyLocal": {IsFile: true, Digest: "d1", Size: 10, ModifyTimestamp: 100},
		"sameBoth":  {IsFile: true, Digest: "same-digest", Size: 20, ModifyTimestamp: 200},
		"digestDiffer": {
			IsFile: true, Digest: "local-digest", Size: 30, ModifyTimestamp: 300,
		},
		"sizeAndModifyDiffer": {
			IsFile: true, Digest: "", Size: 40, ModifyTimestamp: 400,
		},
		"modifyOnlyDiffer": {
			IsFile: true, Digest: "", Size: 50, ModifyTimestamp: 500,
		},
		"deleteExists": {
			IsFile: true, Digest: "same2", Size: 60, ModifyTimestamp: 600, DeleteTimestamp: 0,
		},
		"typeMismatch": {IsFile: true, Digest: "x", Size: 5, ModifyTimestamp: 5},
	}

	remote := map[string]ItemInfo{
		"onlyRemote": {IsFile: true, Digest: "r1", Size: 11, ModifyTimestamp: 110},
		"sameBoth":   {IsFile: true, Digest: "same-digest", Size: 20, ModifyTimestamp: 200},
		"digestDiffer": {
			IsFile: true, Digest: "remote-digest", Size: 30, ModifyTimestamp: 300,
		},
		"sizeAndModifyDiffer": {
			IsFile: true, Digest: "", Size: 41, ModifyTimestamp: 401,
		},
		"modifyOnlyDiffer": {
			IsFile: true, Digest: "", Size: 50, ModifyTimestamp: 501,
		},
		"deleteExists": {
			IsFile: true, Digest: "same2", Size: 60, ModifyTimestamp: 600, DeleteTimestamp: 999,
		},
		"typeMismatch": {IsFile: false, Digest: "", Size: 0, ModifyTimestamp: 0},
	}

	diffMap := ComparePathMapsMulti(local, remote)

	// onlyLocal: present only in local -> PathMissing, Local set, Remote nil.
	diffs, ok := diffMap["onlyLocal"]
	if !ok || len(diffs) != 1 {
		t.Fatalf("onlyLocal: got %v, want exactly one diff", diffs)
	}
	if diffs[0].Diff != PathMissing || diffs[0].Local == nil || diffs[0].Remote != nil {
		t.Errorf("onlyLocal: got %+v, want {PathMissing, Local!=nil, Remote==nil}", diffs[0])
	}
	if diffs[0].Local.Digest != "d1" {
		t.Errorf("onlyLocal: Local.Digest = %q, want %q", diffs[0].Local.Digest, "d1")
	}

	// onlyRemote: present only in remote -> PathMissing, Local nil, Remote set.
	diffs, ok = diffMap["onlyRemote"]
	if !ok || len(diffs) != 1 {
		t.Fatalf("onlyRemote: got %v, want exactly one diff", diffs)
	}
	if diffs[0].Diff != PathMissing || diffs[0].Local != nil || diffs[0].Remote == nil {
		t.Errorf("onlyRemote: got %+v, want {PathMissing, Local==nil, Remote!=nil}", diffs[0])
	}
	if diffs[0].Remote.Digest != "r1" {
		t.Errorf("onlyRemote: Remote.Digest = %q, want %q", diffs[0].Remote.Digest, "r1")
	}

	// sameBoth: identical in both maps -> no diff entry at all for this key.
	if diffs, ok := diffMap["sameBoth"]; ok {
		t.Errorf("sameBoth: expected no diff entry, got %v", diffs)
	}

	// digestDiffer: only Digest differs -> exactly one DigestDifferent diff.
	diffs, ok = diffMap["digestDiffer"]
	if !ok || len(diffs) != 1 {
		t.Fatalf("digestDiffer: got %v, want exactly one diff", diffs)
	}
	if diffs[0].Diff != DigestDifferent {
		t.Errorf("digestDiffer: got Diff=%v, want %v", diffs[0].Diff, DigestDifferent)
	}

	// sizeAndModifyDiffer: Size and ModifyTimestamp both differ -> both
	// SizeDifferent and ModifyTimeDifferent are emitted, in that order.
	diffs, ok = diffMap["sizeAndModifyDiffer"]
	if !ok || len(diffs) != 2 {
		t.Fatalf("sizeAndModifyDiffer: got %v, want exactly two diffs", diffs)
	}
	if diffs[0].Diff != SizeDifferent {
		t.Errorf("sizeAndModifyDiffer: diffs[0] = %v, want %v", diffs[0].Diff, SizeDifferent)
	}
	if diffs[1].Diff != ModifyTimeDifferent {
		t.Errorf("sizeAndModifyDiffer: diffs[1] = %v, want %v", diffs[1].Diff, ModifyTimeDifferent)
	}

	// modifyOnlyDiffer: ModifyTimestamp differs but Size does NOT -> per
	// the source's exact condition (mtime check is AND'd with a size
	// mismatch), no ModifyTimeDifferent (or any other) diff is emitted.
	if diffs, ok := diffMap["modifyOnlyDiffer"]; ok {
		t.Errorf("modifyOnlyDiffer: expected no diff entry (size unchanged), got %v", diffs)
	}

	// deleteExists: everything else matches, but one side has a nonzero
	// DeleteTimestamp -> DeleteTimeExists.
	diffs, ok = diffMap["deleteExists"]
	if !ok || len(diffs) != 1 {
		t.Fatalf("deleteExists: got %v, want exactly one diff", diffs)
	}
	if diffs[0].Diff != DeleteTimeExists {
		t.Errorf("deleteExists: got Diff=%v, want %v", diffs[0].Diff, DeleteTimeExists)
	}

	// typeMismatch: local is a file, remote is a folder at the same path
	// -> FileMissing.
	diffs, ok = diffMap["typeMismatch"]
	if !ok || len(diffs) != 1 {
		t.Fatalf("typeMismatch: got %v, want exactly one diff", diffs)
	}
	if diffs[0].Diff != FileMissing {
		t.Errorf("typeMismatch: got Diff=%v, want %v", diffs[0].Diff, FileMissing)
	}

	// Exactly the expected set of keys should carry a diff.
	wantKeys := map[string]bool{
		"onlyLocal": true, "onlyRemote": true, "digestDiffer": true,
		"sizeAndModifyDiffer": true, "deleteExists": true, "typeMismatch": true,
	}
	for k := range diffMap {
		if !wantKeys[k] {
			t.Errorf("unexpected diff entry for key %q: %v", k, diffMap[k])
		}
	}
	for k := range wantKeys {
		if _, ok := diffMap[k]; !ok {
			t.Errorf("missing expected diff entry for key %q", k)
		}
	}
}
