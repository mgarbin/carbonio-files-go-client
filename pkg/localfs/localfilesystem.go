package localfs

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const maxDigestSize = 1_073_741_824 // 1GB in bytes

type DiffType string

const (
	PathMissing         DiffType = "PathMissing"
	FileMissing         DiffType = "FileMissing"
	DigestDifferent     DiffType = "DigestDifferent"
	SizeDifferent       DiffType = "SizeDifferent"
	ModifyTimeDifferent DiffType = "ModifyTimeDifferent"
	DeleteTimeExists    DiffType = "DeleteTimeExists"
)

type ItemDiff struct {
	Diff   DiffType
	Local  *ItemInfo // item from first map
	Remote *ItemInfo // item from second map
}

type ItemInfo struct {
	IsFile          bool    // true if file, false if directory
	NodeId          string  // unique node ID
	FileVersion     int     // file version (only for files)
	Size            float64 // file size (only for files)
	Digest          string  // file digest (only for files)
	ModifyTimestamp int64   // Unix timestamp (only for files)
	DeleteTimestamp int64   // Unix timestamp (only for files, 0 if not deleted)
}

func Sha384Base64(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha512.New384()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	hash := hasher.Sum(nil) // []byte, binary SHA-384
	encoded := base64.StdEncoding.EncodeToString(hash)
	// Replace all '/' with ','
	encoded = strings.ReplaceAll(encoded, "/", ",")
	return encoded, nil
}

func ReadFolderRecursive(root string) (map[string]ItemInfo, error) {
	items := make(map[string]ItemInfo)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		item := ItemInfo{
			IsFile: !d.IsDir(),
		}

		if d.IsDir() {
			items[relPath] = item
		} else {
			item.Size = float64(info.Size())
			item.FileVersion = 1
			item.ModifyTimestamp = info.ModTime().Unix()
			item.DeleteTimestamp = 0

			if item.Size <= maxDigestSize {
				digest, err := Sha384Base64(path)
				if err != nil {
					item.Digest = ""
				} else {
					item.Digest = digest
				}
			} else {
				item.Digest = "" // or "SKIPPED"
			}
			items[relPath] = item
		}

		return nil
	})
	return items, err
}

func ComparePathMapsMulti(local, remote map[string]ItemInfo) map[string][]ItemDiff {
	diffMap := make(map[string][]ItemDiff)

	// Check items in left against right
	for path, localInfo := range local {
		remoteInfo, exists := remote[path]
		var diffs []ItemDiff
		if !exists {
			diffs = append(diffs, ItemDiff{Diff: PathMissing, Local: &localInfo, Remote: nil})
		} else {
			if localInfo.IsFile && !remoteInfo.IsFile {
				diffs = append(diffs, ItemDiff{Diff: FileMissing, Local: &localInfo, Remote: &remoteInfo})
			} else if !localInfo.IsFile && remoteInfo.IsFile {
				diffs = append(diffs, ItemDiff{Diff: FileMissing, Local: &localInfo, Remote: &remoteInfo})
			} else if localInfo.IsFile && remoteInfo.IsFile {
				// Digest
				if localInfo.Digest != "" && remoteInfo.Digest != "" && localInfo.Digest != remoteInfo.Digest {
					diffs = append(diffs, ItemDiff{Diff: DigestDifferent, Local: &localInfo, Remote: &remoteInfo})
				}
				// Size
				if localInfo.Size != remoteInfo.Size {
					diffs = append(diffs, ItemDiff{Diff: SizeDifferent, Local: &localInfo, Remote: &remoteInfo})
				}
				// Modify timestamp
				if localInfo.ModifyTimestamp != remoteInfo.ModifyTimestamp && localInfo.Size != remoteInfo.Size {
					diffs = append(diffs, ItemDiff{Diff: ModifyTimeDifferent, Local: &localInfo, Remote: &remoteInfo})
				}
				// Delete timestamp
				if localInfo.DeleteTimestamp != 0 || remoteInfo.DeleteTimestamp != 0 {
					diffs = append(diffs, ItemDiff{Diff: DeleteTimeExists, Local: &localInfo, Remote: &remoteInfo})
				}
			}
		}
		if len(diffs) > 0 {
			diffMap[path] = diffs
		}
	}

	// Check for paths in right not in left
	for path, remoteInfo := range remote {
		if _, exists := local[path]; !exists {
			diffMap[path] = append(diffMap[path], ItemDiff{Diff: PathMissing, Local: nil, Remote: &remoteInfo})
		}
	}

	return diffMap
}

// NormalizeRelativePath normalizza path rispetto a root:
// - pulisce . e .. con filepath.Clean
// - tenta di rendere relativo a root con filepath.Rel
// - normalizza Unicode in NFC
// - usa '/' come separatore in DB
// - rimuove trailing slash e "./"
// Restituisce anche original (input) così com'è per l'UI/logging.
func NormalizeRelativePath(root, path string) (relative string, original string, err error) {
	orig := path

	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root, path))
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		// se non possiamo relativizzare, usa il percorso assoluto "clean"
		rel = abs
	}

	// Normalize NFC
	rel = norm.NFC.String(rel)

	// Convert separators to forward slash for DB consistency
	rel = filepath.ToSlash(rel)

	// Strip leading "./" and trailing '/'
	rel = strings.TrimPrefix(rel, "./")
	if rel == "." {
		rel = ""
	}
	rel = strings.TrimSuffix(rel, "/")

	return rel, orig, nil
}

// PathHash ritorna SHA256 hex del relative_path codificato in UTF-8.
// Usato per indici con lunghezza fissa.
func PathHash(relative string) string {
	h := sha256.Sum256([]byte(relative))
	return hex.EncodeToString(h[:])
}
