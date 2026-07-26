package actions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"carbonio-files-go-client/pkg/appdir"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
)

// newEmptyRemoteTreeServer starts a self-signed TLS server that answers
// every GraphQL request with an empty remote tree (getNode: null), the
// same shape graphql.GraphQLAuthenticator.GetAllNode treats as "no
// children" (see graphqlAPI.go: resp.GetNode == nil returns nil, nil).
// It isolates these tests from any real Carbonio server: the point is
// exercising updateCacheSync's local-side deletion detection, not the
// remote listing itself. Every tracked record's remote_path is therefore
// left out of the (empty) remote tree on purpose - these tests assert on
// the LocalDeleted field directly rather than through QueryLocalDeleted
// (which also requires remote_deleted=0) so that is harmless.
func newEmptyRemoteTreeServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"getNode": nil}})
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

// localDeletedFlag returns the local_deleted flag of the sole tracked
// record after an UpdateCacheSync run.
func localDeletedFlag(t *testing.T, cacheDb *sqlitecache.SqliteHelper) int {
	t.Helper()
	records, err := cacheDb.QueryAll()
	if err != nil {
		t.Fatalf("QueryAll: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("QueryAll() = %d records, want 1", len(records))
	}
	return records[0].LocalDeleted
}

// TestUpdateCacheSync_DoesNotFlagLocalDeletedWhenWalkSkipsAnExistingPath
// covers updateCacheSync's os.Lstat fallback: a tracked folder that is
// still fully present on disk must never be flagged local_deleted just
// because localfs.ReadFolderRecursive's bulk walk didn't surface it as a
// map key. "~archive" is used as the reproducer because it's the one
// walk-skip localfs.ReadFolderRecursive documents (showHidden=false skips
// any "."/"~"-prefixed entry, the "~" branch existing for Windows temp
// files) that is fully portable across CI platforms - unlike the
// Windows/macOS case-insensitive-filesystem rename this fix also guards
// against, which can't be reproduced on a case-sensitive Linux CI
// filesystem. Both are the same failure mode: the folder is not gone, the
// bulk map merely didn't include it under this exact key. Before this
// fix, updateCacheSync trusted the map miss outright and flagged
// local_deleted=1, which LiveCacheSync's QueryLocalDeleted-driven loop
// would then use to trash the folder's still-synced remote counterpart.
func TestUpdateCacheSync_DoesNotFlagLocalDeletedWhenWalkSkipsAnExistingPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	endpoint := newEmptyRemoteTreeServer(t)

	localFolder := t.TempDir()
	trackedRelPath := "~archive"
	if err := os.MkdirAll(filepath.Join(localFolder, trackedRelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cacheDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		t.Fatalf("NewSqliteHelper: %v", err)
	}
	defer cacheDb.Close()

	if _, err := cacheDb.InsertFileSync(
		"node-1", "", "remote/"+trackedRelPath, "rh", trackedRelPath, "lh",
		true, "", "", 0, 0, "", "", "synced", "",
		0, 0, false, false, false, "",
	); err != nil {
		t.Fatalf("InsertFileSync: %v", err)
	}

	if err := UpdateCacheSync(endpoint, "token", localFolder); err != nil {
		t.Fatalf("UpdateCacheSync: %v", err)
	}

	if got := localDeletedFlag(t, cacheDb); got != 0 {
		t.Fatalf("LocalDeleted = %d, want 0: %q is still on disk but would have its remote counterpart trashed", got, trackedRelPath)
	}
}

// TestUpdateCacheSync_StillFlagsLocalDeletedWhenPathIsGenuinelyGone locks
// in the companion behavior: a tracked path that is truly absent from
// disk (os.Lstat also fails) must still be flagged local_deleted, so the
// os.Lstat fallback added for the case above doesn't silently disable
// real local-deletion propagation.
func TestUpdateCacheSync_StillFlagsLocalDeletedWhenPathIsGenuinelyGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	endpoint := newEmptyRemoteTreeServer(t)

	localFolder := t.TempDir()
	trackedRelPath := "gone"
	// Deliberately never created on disk.

	cacheDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		t.Fatalf("NewSqliteHelper: %v", err)
	}
	defer cacheDb.Close()

	if _, err := cacheDb.InsertFileSync(
		"node-1", "", "remote/"+trackedRelPath, "rh", trackedRelPath, "lh",
		true, "", "", 0, 0, "", "", "synced", "",
		0, 0, false, false, false, "",
	); err != nil {
		t.Fatalf("InsertFileSync: %v", err)
	}

	if err := UpdateCacheSync(endpoint, "token", localFolder); err != nil {
		t.Fatalf("UpdateCacheSync: %v", err)
	}

	if got := localDeletedFlag(t, cacheDb); got != 1 {
		t.Fatalf("LocalDeleted = %d, want 1: a genuinely-removed path must still propagate as local_deleted", got)
	}
}
