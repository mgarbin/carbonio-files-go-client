package carbonio

import (
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsValidEmail locks in that isValidEmail is a thin wrapper around
// mail.ParseAddress: anything RFC 5322 can parse as an address is "valid",
// everything else is rejected.
func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{"plain address", "user@example.com", true},
		{"display name address", "Someone <someone@example.com>", true},
		{"subdomain address", "user@mail.example.com", true},
		{"empty string", "", false},
		{"no at sign", "not-an-email", false},
		{"missing local part", "@example.com", false},
		{"missing domain", "user@", false},
		{"garbage with spaces", "this is not an email", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidEmail(tt.email); got != tt.want {
				t.Errorf("isValidEmail(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

// TestSha384Base64_Success verifies Sha384Base64 against an independently
// computed base64(sha384(content)) so a regression in either the hash
// algorithm or the encoding step is caught.
func TestSha384Base64_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "content.txt")
	content := []byte("the quick brown fox jumps over the lazy dog")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	hasher := sha512.New384()
	hasher.Write(content)
	want := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

	got, err := Sha384Base64(path)
	if err != nil {
		t.Fatalf("Sha384Base64() error = %v, want nil", err)
	}
	if got != want {
		t.Fatalf("Sha384Base64() = %q, want %q", got, want)
	}
}

// TestSha384Base64_NonexistentFile verifies the error path when the file
// cannot be opened.
func TestSha384Base64_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	got, err := Sha384Base64(path)
	if err == nil {
		t.Fatalf("Sha384Base64() error = nil, want non-nil")
	}
	if got != "" {
		t.Fatalf("Sha384Base64() = %q, want empty string on error", got)
	}
}

// TestDetectMimeType_Text verifies that DetectMimeType surfaces exactly
// what net/http.DetectContentType reports for a plain text payload.
func TestDetectMimeType_Text(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "text.txt")
	content := []byte(strings.Repeat("hello world ", 10))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	want := http.DetectContentType(content)

	got, err := DetectMimeType(path)
	if err != nil {
		t.Fatalf("DetectMimeType() error = %v, want nil", err)
	}
	if got != want {
		t.Fatalf("DetectMimeType() = %q, want %q", got, want)
	}
}

// TestDetectMimeType_PNGSignature verifies that a distinctive byte
// signature (the PNG magic header) is detected the same way
// http.DetectContentType would detect it directly.
func TestDetectMimeType_PNGSignature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	// PNG magic bytes followed by a bit of filler so the file is
	// unambiguously non-empty.
	content := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, []byte("filler-bytes-after-signature")...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	want := http.DetectContentType(content)
	if want != "image/png" {
		t.Fatalf("test setup invalid: http.DetectContentType(content) = %q, want image/png", want)
	}

	got, err := DetectMimeType(path)
	if err != nil {
		t.Fatalf("DetectMimeType() error = %v, want nil", err)
	}
	if got != want {
		t.Fatalf("DetectMimeType() = %q, want %q", got, want)
	}
}

// TestDetectMimeType_EmptyFileReturnsEOF locks in the current behavior of
// DetectMimeType: it performs a single file.Read into a 512-byte buffer and
// returns any error verbatim, with no io.EOF special-case. A zero-byte
// regular file's first Read returns (0, io.EOF) on this platform (verified
// independently below), so DetectMimeType must propagate that as an error
// rather than returning a successful "empty" MIME type.
func TestDetectMimeType_EmptyFileReturnsEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Confirm the assumption this test relies on: reading an empty regular
	// file returns io.EOF on the first Read, not (0, nil).
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	buf := make([]byte, 512)
	n, readErr := f.Read(buf)
	f.Close()
	if n != 0 || !errors.Is(readErr, io.EOF) {
		t.Fatalf("assumption violated: empty file Read() = (%d, %v), want (0, io.EOF)", n, readErr)
	}

	got, err := DetectMimeType(path)
	if err == nil {
		t.Fatalf("DetectMimeType() error = nil, want io.EOF")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("DetectMimeType() error = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("DetectMimeType() = %q, want empty string on error", got)
	}
}

// TestDetectMimeType_NonexistentFile verifies the error path when the file
// cannot be opened.
func TestDetectMimeType_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.bin")

	got, err := DetectMimeType(path)
	if err == nil {
		t.Fatalf("DetectMimeType() error = nil, want non-nil")
	}
	if got != "" {
		t.Fatalf("DetectMimeType() = %q, want empty string on error", got)
	}
}

// TestExtractFileName exercises filepath.Base semantics: directories,
// trailing slashes, and bare filenames.
func TestExtractFileName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"nested path", "/a/b/c.txt", "c.txt"},
		{"bare filename", "c.txt", "c.txt"},
		{"trailing slash", "/a/b/", "b"},
		{"root", "/", "/"},
		{"empty path", "", "."},
		{"relative nested path", "dir/subdir/file.ext", "file.ext"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractFileName(tt.path); got != tt.want {
				t.Errorf("ExtractFileName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestGetFileContentLength_Success verifies the reported size matches the
// actual number of bytes written to the file.
func TestGetFileContentLength_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sized.txt")
	content := []byte("some content of a known length")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := GetFileContentLength(path)
	if err != nil {
		t.Fatalf("GetFileContentLength() error = %v, want nil", err)
	}
	if got != int64(len(content)) {
		t.Fatalf("GetFileContentLength() = %d, want %d", got, len(content))
	}
}

// TestGetFileContentLength_NonexistentFile verifies the error path when the
// file does not exist.
func TestGetFileContentLength_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	got, err := GetFileContentLength(path)
	if err == nil {
		t.Fatalf("GetFileContentLength() error = nil, want non-nil")
	}
	if got != 0 {
		t.Fatalf("GetFileContentLength() = %d, want 0 on error", got)
	}
}

// TestProgressBar locks in progressBar's exact rendering: bars = percent/2
// "=" characters followed by enough spaces to always total 50 columns.
func TestProgressBar(t *testing.T) {
	tests := []struct {
		percent int
		want    string
	}{
		{0, strings.Repeat(" ", 50)},
		{50, strings.Repeat("=", 25) + strings.Repeat(" ", 25)},
		{100, strings.Repeat("=", 50)},
	}

	for _, tt := range tests {
		got := progressBar(tt.percent)
		if got != tt.want {
			t.Errorf("progressBar(%d) = %q, want %q", tt.percent, got, tt.want)
		}
		if len(got) != 50 {
			t.Errorf("progressBar(%d) length = %d, want 50", tt.percent, len(got))
		}
	}
}

// TestStringRepeat exercises stringRepeat against the stdlib's
// strings.Repeat as an independent oracle, including the count == 0 edge
// case (must return "").
func TestStringRepeat(t *testing.T) {
	tests := []struct {
		s     string
		count int
	}{
		{"=", 0},
		{"=", 1},
		{"ab", 5},
		{" ", 10},
	}

	for _, tt := range tests {
		want := strings.Repeat(tt.s, tt.count)
		if got := stringRepeat(tt.s, tt.count); got != want {
			t.Errorf("stringRepeat(%q, %d) = %q, want %q", tt.s, tt.count, got, want)
		}
	}
}
