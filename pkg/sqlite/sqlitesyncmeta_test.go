package sqlitecache

import "testing"

func TestSyncMetaCRUD(t *testing.T) {
	h, _ := newTestHelper(t)

	if meta, err := h.GetSyncMeta(); err != nil || meta != nil {
		t.Fatalf("GetSyncMeta() before any run = (%+v, %v), want (nil, nil)", meta, err)
	}

	if err := h.SetSyncRunResult("2026-01-01T10:00:00Z", "network unreachable"); err != nil {
		t.Fatalf("SetSyncRunResult() (failed run) error = %v", err)
	}
	meta, err := h.GetSyncMeta()
	if err != nil {
		t.Fatalf("GetSyncMeta() error = %v", err)
	}
	if meta == nil || meta.LastRunAt != "2026-01-01T10:00:00Z" || meta.LastError != "network unreachable" {
		t.Fatalf("GetSyncMeta() after failed run = %+v, want {LastRunAt: 2026-01-01T10:00:00Z, LastError: network unreachable}", meta)
	}

	// A later successful run must upsert (replace, not accumulate) the
	// singleton row - the dashboard only ever needs the *last* outcome.
	if err := h.SetSyncRunResult("2026-01-01T10:05:00Z", ""); err != nil {
		t.Fatalf("SetSyncRunResult() (successful run) error = %v", err)
	}
	meta, err = h.GetSyncMeta()
	if err != nil {
		t.Fatalf("GetSyncMeta() error = %v", err)
	}
	if meta == nil || meta.LastRunAt != "2026-01-01T10:05:00Z" || meta.LastError != "" {
		t.Fatalf("GetSyncMeta() after successful run = %+v, want {LastRunAt: 2026-01-01T10:05:00Z, LastError: \"\"}", meta)
	}
}

func TestCountBySyncStatusAndPresence(t *testing.T) {
	h, _ := newTestHelper(t)

	rows := []struct {
		remotePath    string
		localPath     string
		syncStatus    string
		remoteDeleted int
		localDeleted  int
	}{
		{"/a.txt", "/local/a.txt", "remote_only", 0, 0},
		{"/b.txt", "/local/b.txt", "remote_only", 0, 0},
		{"/c.txt", "/local/c.txt", "synced", 0, 0},
		{"/d.txt", "/local/d.txt", "remote_deleted", 1, 0}, // no longer present remotely
		{"", "/local/e.txt", "local_only", 0, 0},           // present locally, never seen remotely
		{"/f.txt", "/local/f.txt", "local_deleted", 0, 1},  // local copy removed, remote still present
	}
	for _, r := range rows {
		if _, err := h.InsertFileSync("node-"+r.remotePath, "", r.remotePath, "", r.localPath, "", false, "", "", 0, 0, "", "", r.syncStatus, "", r.localDeleted, r.remoteDeleted); err != nil {
			t.Fatalf("InsertFileSync(%s): %v", r.remotePath, err)
		}
	}

	if got, err := h.CountBySyncStatus("remote_only"); err != nil || got != 2 {
		t.Fatalf("CountBySyncStatus(remote_only) = (%d, %v), want (2, nil)", got, err)
	}
	if got, err := h.CountBySyncStatus("synced"); err != nil || got != 1 {
		t.Fatalf("CountBySyncStatus(synced) = (%d, %v), want (1, nil)", got, err)
	}
	if got, err := h.CountBySyncStatus("local_only"); err != nil || got != 1 {
		t.Fatalf("CountBySyncStatus(local_only) = (%d, %v), want (1, nil)", got, err)
	}
	// a, b, c, f are present remotely; d is flagged remote_deleted; e has no remote_path.
	if got, err := h.CountRemotePresent(); err != nil || got != 4 {
		t.Fatalf("CountRemotePresent() = (%d, %v), want (4, nil)", got, err)
	}
	// a, b, c, d, e are present locally; f is flagged locally deleted.
	if got, err := h.CountLocalPresent(); err != nil || got != 5 {
		t.Fatalf("CountLocalPresent() = (%d, %v), want (5, nil)", got, err)
	}
}

func TestCountPendingChanges(t *testing.T) {
	h, _ := newTestHelper(t)

	// Nothing to reconcile in an empty cache.
	if got, err := h.CountPendingChanges(); err != nil || got != 0 {
		t.Fatalf("CountPendingChanges() on empty cache = (%d, %v), want (0, nil)", got, err)
	}

	// Fully synced record: not pending.
	if _, err := h.InsertFileSync("n1", "", "/synced.txt", "", "/local/synced.txt", "", false, "", "", 0, 0, "", "", "synced", "", 0, 0); err != nil {
		t.Fatalf("InsertFileSync(synced): %v", err)
	}
	if got, err := h.CountPendingChanges(); err != nil || got != 0 {
		t.Fatalf("CountPendingChanges() with only a synced record = (%d, %v), want (0, nil)", got, err)
	}

	// remote_only, local_only, out_of_sync: each pending.
	if _, err := h.InsertFileSync("n2", "", "/remote-only.txt", "", "", "", false, "", "", 0, 0, "", "", "remote_only", "", 0, 0); err != nil {
		t.Fatalf("InsertFileSync(remote_only): %v", err)
	}
	if _, err := h.InsertFileSync("", "", "", "", "/local-only.txt", "", false, "", "", 0, 0, "", "", "local_only", "", 0, 0); err != nil {
		t.Fatalf("InsertFileSync(local_only): %v", err)
	}
	if _, err := h.InsertFileSync("n3", "", "/out-of-sync.txt", "", "/local/out-of-sync.txt", "", false, "", "", 0, 0, "", "", "out_of_sync", "", 0, 0); err != nil {
		t.Fatalf("InsertFileSync(out_of_sync): %v", err)
	}
	if got, err := h.CountPendingChanges(); err != nil || got != 3 {
		t.Fatalf("CountPendingChanges() with 3 unresolved statuses = (%d, %v), want (3, nil)", got, err)
	}

	// A remote deletion not yet propagated locally (matches QueryRemoteDeleted's
	// condition) counts as pending...
	if _, err := h.InsertFileSync("n4", "", "/remote-deleted.txt", "", "/local/remote-deleted.txt", "", false, "", "", 0, 0, "", "", "synced", "", 0, 1); err != nil {
		t.Fatalf("InsertFileSync(remote_deleted pending): %v", err)
	}
	if got, err := h.CountPendingChanges(); err != nil || got != 4 {
		t.Fatalf("CountPendingChanges() with a pending remote deletion = (%d, %v), want (4, nil)", got, err)
	}

	// ...but once LiveCacheSync has propagated it (local_deleted flips to 1
	// too), it must drop out of the pending count.
	if err := h.UpdateFileSync("node_id", "n4", map[string]interface{}{"local_deleted": 1, "sync_status": "remote_deleted"}); err != nil {
		t.Fatalf("UpdateFileSync(n4): %v", err)
	}
	if got, err := h.CountPendingChanges(); err != nil || got != 3 {
		t.Fatalf("CountPendingChanges() after deletion propagated = (%d, %v), want (3, nil)", got, err)
	}
}
