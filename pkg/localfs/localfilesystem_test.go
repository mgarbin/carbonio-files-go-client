package localfs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadFolderRecursiveUsesForwardSlashKeys locks in the map-key contract
// that ReadFolderRecursive must share with utils.RecursiveListNodeItems
// (remote tree) and the sqlite cache's local_path/remote_path columns:
// relative paths are always "/"-joined, regardless of the OS-native
// separator filepath.Rel would otherwise return (e.g. '\' on Windows).
// Without filepath.ToSlash on the relPath, nested local items get a key
// that can never match their remote counterpart, so every sync run
// re-detects already-synced nested files as brand-new local-only items
// and re-uploads them (see localfilesystem.go's ReadFolderRecursive).
func TestReadFolderRecursiveUsesForwardSlashKeys(t *testing.T) {
	root := t.TempDir()

	nested := filepath.Join(root, "sub", "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(nested, "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	items, err := ReadFolderRecursive(root, false)
	if err != nil {
		t.Fatalf("ReadFolderRecursive failed: %v", err)
	}

	wantKeys := []string{"sub", "sub/inner", "sub/inner/file.txt"}
	for _, k := range wantKeys {
		if _, ok := items[k]; !ok {
			t.Errorf("expected forward-slash key %q in result, got keys: %v", k, keysOf(items))
		}
	}

	// No key may contain a backslash: that would only ever match itself
	// on Windows, never the remote map's "/"-joined paths.
	for k := range items {
		for _, r := range k {
			if r == '\\' {
				t.Errorf("key %q contains a backslash separator; keys must be normalized with filepath.ToSlash", k)
			}
		}
	}
}

func keysOf(m map[string]ItemInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
