package sqlitecache

import "testing"

// TestNewSqliteHelperCreatesEmptyFilesyncTable verifies that opening a fresh
// database creates the filesync table (not just some other table) and that
// it starts out empty - CountRecords must work against a brand new db
// without a prior insert.
func TestNewSqliteHelperCreatesEmptyFilesyncTable(t *testing.T) {
	h, _ := newTestHelper(t)

	count, err := h.CountRecords()
	if err != nil {
		t.Fatalf("CountRecords() on fresh db error = %v", err)
	}
	if count != 0 {
		t.Fatalf("CountRecords() on fresh db = %d, want 0", count)
	}

	records, err := h.QueryAll()
	if err != nil {
		t.Fatalf("QueryAll() on fresh db error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("QueryAll() on fresh db = %+v, want empty slice", records)
	}
}

// TestInsertFileSyncRoundTrip inserts one record with a distinct, non-zero
// value in every column and verifies that reading it back via QueryAll
// reproduces every field exactly - this locks in the column order used by
// both InsertFileSync's positional placeholders and scanFileSyncRows, which
// must stay in lockstep with selectAllColumns.
func TestInsertFileSyncRoundTrip(t *testing.T) {
	h, _ := newTestHelper(t)

	id, err := h.InsertFileSync(
		"node-1", "parent-1", "/remote/path", "remote-hash",
		"/local/path", "local-hash",
		true,
		"2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z",
		1024, 2048,
		"remote-digest", "local-digest", "pending_upload", "2026-01-01T12:00:00Z",
		1, 0,
		true, false, true,
		"application/pdf",
	)
	if err != nil {
		t.Fatalf("InsertFileSync() error = %v", err)
	}
	if id != 1 {
		t.Fatalf("InsertFileSync() id = %d, want 1 (first row in a fresh table)", id)
	}

	records, err := h.QueryAll()
	if err != nil {
		t.Fatalf("QueryAll() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("QueryAll() len = %d, want 1", len(records))
	}

	want := FileSyncRecord{
		ID:                 id,
		NodeID:             "node-1",
		ParentID:           "parent-1",
		RemotePath:         "/remote/path",
		RemotePathHash:     "remote-hash",
		LocalPath:          "/local/path",
		LocalPathHash:      "local-hash",
		IsDirectory:        true,
		RemoteLastModified: "2026-01-01T10:00:00Z",
		LocalLastModified:  "2026-01-01T11:00:00Z",
		RemoteSize:         1024,
		LocalSize:          2048,
		RemoteDigest:       "remote-digest",
		LocalDigest:        "local-digest",
		SyncStatus:         "pending_upload",
		LastSynced:         "2026-01-01T12:00:00Z",
		LocalDeleted:       1,
		RemoteDeleted:      0,
		CanWriteFile:       true,
		CanAddVersion:      false,
		CanDelete:          true,
		MimeType:           "application/pdf",
	}
	if got := records[0]; got != want {
		t.Fatalf("QueryAll()[0] = %+v, want %+v", got, want)
	}
}

// TestUpdateFileSync verifies UpdateFileSync updates only the requested
// fields, leaves every other column untouched, and supports all three
// documented selector fields ("id", "node_id", "local_digest"). It also
// checks that an unsupported selector is rejected and that an empty fields
// map is a no-op rather than an error.
func TestUpdateFileSync(t *testing.T) {
	h, _ := newTestHelper(t)

	id, err := h.InsertFileSync(
		"node-1", "parent-1", "/remote/path", "remote-hash",
		"/local/path", "local-hash",
		false,
		"2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z",
		10, 20,
		"remote-digest", "local-digest", "pending_upload", "2026-01-01T12:00:00Z",
		0, 0,
		false, false, false,
		"text/plain",
	)
	if err != nil {
		t.Fatalf("InsertFileSync() error = %v", err)
	}

	fetch := func() FileSyncRecord {
		t.Helper()
		records, err := h.QueryAll()
		if err != nil {
			t.Fatalf("QueryAll() error = %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("QueryAll() len = %d, want 1", len(records))
		}
		return records[0]
	}

	// selector "id": only sync_status must change.
	if err := h.UpdateFileSync("id", id, map[string]interface{}{"sync_status": "synced"}); err != nil {
		t.Fatalf("UpdateFileSync(id) error = %v", err)
	}
	rec := fetch()
	if rec.SyncStatus != "synced" {
		t.Fatalf("SyncStatus after UpdateFileSync(id) = %q, want %q", rec.SyncStatus, "synced")
	}
	if rec.NodeID != "node-1" || rec.LocalDigest != "local-digest" || rec.RemotePath != "/remote/path" {
		t.Fatalf("UpdateFileSync(id) touched unrelated fields: %+v", rec)
	}

	// selector "node_id": only local_digest must change.
	if err := h.UpdateFileSync("node_id", "node-1", map[string]interface{}{"local_digest": "new-digest"}); err != nil {
		t.Fatalf("UpdateFileSync(node_id) error = %v", err)
	}
	rec = fetch()
	if rec.LocalDigest != "new-digest" {
		t.Fatalf("LocalDigest after UpdateFileSync(node_id) = %q, want %q", rec.LocalDigest, "new-digest")
	}
	if rec.SyncStatus != "synced" || rec.RemotePath != "/remote/path" {
		t.Fatalf("UpdateFileSync(node_id) touched unrelated fields: %+v", rec)
	}

	// selector "local_digest": multiple fields at once.
	if err := h.UpdateFileSync("local_digest", "new-digest", map[string]interface{}{
		"remote_path":    "/remote/path/renamed",
		"local_deleted":  1,
		"remote_deleted": 0,
	}); err != nil {
		t.Fatalf("UpdateFileSync(local_digest) error = %v", err)
	}
	rec = fetch()
	if rec.RemotePath != "/remote/path/renamed" || rec.LocalDeleted != 1 {
		t.Fatalf("UpdateFileSync(local_digest) did not apply fields: %+v", rec)
	}
	if rec.NodeID != "node-1" || rec.LocalDigest != "new-digest" {
		t.Fatalf("UpdateFileSync(local_digest) touched unrelated fields: %+v", rec)
	}

	// Unsupported selector field must error and must not modify anything.
	if err := h.UpdateFileSync("remote_path", "/remote/path/renamed", map[string]interface{}{"sync_status": "should_not_apply"}); err == nil {
		t.Fatalf("UpdateFileSync() with invalid selector field = nil error, want an error")
	}
	if rec := fetch(); rec.SyncStatus != "synced" {
		t.Fatalf("UpdateFileSync() with invalid selector mutated the row: %+v", rec)
	}

	// Empty fields map is a documented no-op, not an error.
	before := fetch()
	if err := h.UpdateFileSync("id", id, map[string]interface{}{}); err != nil {
		t.Fatalf("UpdateFileSync() with empty fields map error = %v", err)
	}
	if after := fetch(); after != before {
		t.Fatalf("UpdateFileSync() with empty fields map changed the row: before=%+v after=%+v", before, after)
	}
}

// TestQueryBySyncStatus verifies filtering by sync_status only returns rows
// with an exact match, across a table containing several distinct statuses.
func TestQueryBySyncStatus(t *testing.T) {
	h, _ := newTestHelper(t)

	insert := func(remotePath, status string) {
		t.Helper()
		if _, err := h.InsertFileSync(
			"node-"+remotePath, "parent", remotePath, "rh", "/local"+remotePath, "lh",
			false, "", "", 0, 0, "", "", status, "",
			0, 0, false, false, false, "",
		); err != nil {
			t.Fatalf("InsertFileSync() error = %v", err)
		}
	}

	insert("/a", "pending_upload")
	insert("/b", "synced")
	insert("/c", "pending_upload")
	insert("/d", "error")

	records, err := h.QueryBySyncStatus("pending_upload")
	if err != nil {
		t.Fatalf("QueryBySyncStatus() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("QueryBySyncStatus(pending_upload) len = %d, want 2 (got %+v)", len(records), records)
	}
	for _, rec := range records {
		if rec.SyncStatus != "pending_upload" {
			t.Fatalf("QueryBySyncStatus(pending_upload) returned a row with status %q", rec.SyncStatus)
		}
		if rec.RemotePath != "/a" && rec.RemotePath != "/c" {
			t.Fatalf("QueryBySyncStatus(pending_upload) returned unexpected row %+v", rec)
		}
	}

	records, err = h.QueryBySyncStatus("does_not_exist")
	if err != nil {
		t.Fatalf("QueryBySyncStatus() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("QueryBySyncStatus(does_not_exist) len = %d, want 0", len(records))
	}
}

// TestQueryRemoteDeleted verifies the exact documented predicate
// (remote_deleted=1, local_deleted=0, local_path != ”) and excludes every
// row that fails one of the three conditions.
func TestQueryRemoteDeleted(t *testing.T) {
	h, _ := newTestHelper(t)

	type row struct {
		path                        string
		localDeleted, remoteDeleted int
		localPath                   string
	}
	rows := []row{
		{"/match", 0, 1, "/local/match"},   // matches
		{"/both-deleted", 1, 1, "/local1"}, // local_deleted != 0
		{"/no-local-path", 0, 1, ""},       // local_path empty
		{"/not-remote-deleted", 0, 0, "/local2"},
	}
	for _, r := range rows {
		if _, err := h.InsertFileSync(
			"node", "parent", r.path, "rh", r.localPath, "lh",
			false, "", "", 0, 0, "", "", "", "",
			r.localDeleted, r.remoteDeleted, false, false, false, "",
		); err != nil {
			t.Fatalf("InsertFileSync() error = %v", err)
		}
	}

	records, err := h.QueryRemoteDeleted()
	if err != nil {
		t.Fatalf("QueryRemoteDeleted() error = %v", err)
	}
	if len(records) != 1 || records[0].RemotePath != "/match" {
		t.Fatalf("QueryRemoteDeleted() = %+v, want exactly the /match row", records)
	}
}

// TestQueryLocalDeleted mirrors TestQueryRemoteDeleted for the
// local_deleted=1/remote_deleted=0/node_id!=”/remote_path!=” predicate.
func TestQueryLocalDeleted(t *testing.T) {
	h, _ := newTestHelper(t)

	type row struct {
		nodeID, remotePath          string
		localDeleted, remoteDeleted int
	}
	rows := []row{
		{"node-1", "/match", 1, 0},        // matches
		{"node-2", "/both-deleted", 1, 1}, // remote_deleted != 0
		{"", "/no-node-id", 1, 0},         // node_id empty
		{"node-3", "", 1, 0},              // remote_path empty
		{"node-4", "/not-local-deleted", 0, 0},
	}
	for _, r := range rows {
		if _, err := h.InsertFileSync(
			r.nodeID, "parent", r.remotePath, "rh", "/local", "lh",
			false, "", "", 0, 0, "", "", "", "",
			r.localDeleted, r.remoteDeleted, false, false, false, "",
		); err != nil {
			t.Fatalf("InsertFileSync() error = %v", err)
		}
	}

	records, err := h.QueryLocalDeleted()
	if err != nil {
		t.Fatalf("QueryLocalDeleted() error = %v", err)
	}
	if len(records) != 1 || records[0].RemotePath != "/match" || records[0].NodeID != "node-1" {
		t.Fatalf("QueryLocalDeleted() = %+v, want exactly the /match row", records)
	}
}

// TestQueryFolderByPath verifies folder lookup matches on either
// remote_path or local_path (while requiring is_directory, a non-empty
// node_id and neither deletion flag set), and that a miss returns
// (nil, nil) rather than an error.
func TestQueryFolderByPath(t *testing.T) {
	h, _ := newTestHelper(t)

	type row struct {
		nodeID, remotePath, localPath string
		isDirectory                   bool
		localDeleted, remoteDeleted   int
	}
	rows := []row{
		{"node-remote", "/folders/by-remote", "/local/other", true, 0, 0},
		{"node-local", "/remote/other", "/folders/by-local", true, 0, 0},
		{"", "/folders/no-node-id", "/local/no-node-id", true, 0, 0},
		{"node-file", "/folders/is-a-file", "/local/is-a-file", false, 0, 0},
		{"node-deleted", "/folders/deleted", "/local/deleted", true, 1, 0},
	}
	for _, r := range rows {
		if _, err := h.InsertFileSync(
			r.nodeID, "parent", r.remotePath, "rh", r.localPath, "lh",
			r.isDirectory, "", "", 0, 0, "", "", "", "",
			r.localDeleted, r.remoteDeleted, false, false, false, "",
		); err != nil {
			t.Fatalf("InsertFileSync() error = %v", err)
		}
	}

	rec, err := h.QueryFolderByPath("/folders/by-remote")
	if err != nil {
		t.Fatalf("QueryFolderByPath(by-remote) error = %v", err)
	}
	if rec == nil || rec.NodeID != "node-remote" {
		t.Fatalf("QueryFolderByPath(by-remote) = %+v, want node-remote", rec)
	}

	rec, err = h.QueryFolderByPath("/folders/by-local")
	if err != nil {
		t.Fatalf("QueryFolderByPath(by-local) error = %v", err)
	}
	if rec == nil || rec.NodeID != "node-local" {
		t.Fatalf("QueryFolderByPath(by-local) = %+v, want node-local", rec)
	}

	for _, path := range []string{"/folders/no-node-id", "/folders/is-a-file", "/folders/deleted", "/does/not/exist"} {
		rec, err := h.QueryFolderByPath(path)
		if err != nil {
			t.Fatalf("QueryFolderByPath(%s) error = %v", path, err)
		}
		if rec != nil {
			t.Fatalf("QueryFolderByPath(%s) = %+v, want nil", path, rec)
		}
	}
}

// TestDeleteAllAndResetAutoIncrement verifies the table is emptied and the
// AUTOINCREMENT counter is reset, so the next inserted row starts back at
// id 1 instead of continuing from wherever the deleted rows left off.
func TestDeleteAllAndResetAutoIncrement(t *testing.T) {
	h, _ := newTestHelper(t)

	for range 3 {
		if _, err := h.InsertFileSync(
			"node", "parent", "/path", "rh", "/local", "lh",
			false, "", "", 0, 0, "", "", "", "",
			0, 0, false, false, false, "",
		); err != nil {
			t.Fatalf("InsertFileSync() error = %v", err)
		}
	}
	if count, err := h.CountRecords(); err != nil || count != 3 {
		t.Fatalf("CountRecords() before delete = (%d, %v), want (3, nil)", count, err)
	}

	if err := h.DeleteAllAndResetAutoIncrement(); err != nil {
		t.Fatalf("DeleteAllAndResetAutoIncrement() error = %v", err)
	}
	if count, err := h.CountRecords(); err != nil || count != 0 {
		t.Fatalf("CountRecords() after delete = (%d, %v), want (0, nil)", count, err)
	}

	id, err := h.InsertFileSync(
		"node", "parent", "/path", "rh", "/local", "lh",
		false, "", "", 0, 0, "", "", "", "",
		0, 0, false, false, false, "",
	)
	if err != nil {
		t.Fatalf("InsertFileSync() after reset error = %v", err)
	}
	if id != 1 {
		t.Fatalf("InsertFileSync() id after DeleteAllAndResetAutoIncrement() = %d, want 1", id)
	}
}

// TestSqliteHelperClose verifies Close() reports no error for a healthy,
// freshly opened database handle.
func TestSqliteHelperClose(t *testing.T) {
	h, _ := newTestHelper(t)

	if err := h.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
