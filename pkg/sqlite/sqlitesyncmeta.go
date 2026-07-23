package sqlitecache

import "database/sql"

// SyncMeta holds the outcome of the most recently completed UpdateCacheSync
// run: when it finished and the error message it returned, if any. It backs
// the GUI dashboard's sync status panel (last sync time + error banner),
// independent of the per-file rows in the filesync table.
type SyncMeta struct {
	LastRunAt string
	LastError string
}

// ensureSyncMetaTable creates the "sync_meta" table: a singleton (id is
// always 1) that tracks the last UpdateCacheSync run, mirroring the
// singleton pattern used by the "config" table (see ensureConfigTable).
func ensureSyncMetaTable(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS sync_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_run_at TEXT NOT NULL,
		last_error TEXT NOT NULL DEFAULT ''
	);`)
	return err
}

// GetSyncMeta returns the outcome of the last recorded UpdateCacheSync run,
// or (nil, nil) if the scan has never run yet.
func (h *SqliteHelper) GetSyncMeta() (*SyncMeta, error) {
	row := h.DB.QueryRow(`SELECT last_run_at, last_error FROM sync_meta WHERE id = 1`)
	var meta SyncMeta
	if err := row.Scan(&meta.LastRunAt, &meta.LastError); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &meta, nil
}

// SetSyncRunResult upserts the outcome of the most recent UpdateCacheSync
// run. lastError is the empty string when the run succeeded.
func (h *SqliteHelper) SetSyncRunResult(lastRunAt, lastError string) error {
	_, err := h.DB.Exec(`
	INSERT INTO sync_meta (id, last_run_at, last_error) VALUES (1, ?, ?)
	ON CONFLICT(id) DO UPDATE SET last_run_at = excluded.last_run_at, last_error = excluded.last_error`,
		lastRunAt, lastError)
	return err
}

// CountBySyncStatus returns the number of filesync records with the given
// sync_status value (e.g. "remote_only" for items still missing locally).
func (h *SqliteHelper) CountBySyncStatus(status string) (int, error) {
	var count int
	err := h.DB.QueryRow(`SELECT COUNT(*) FROM filesync WHERE sync_status = ?`, status).Scan(&count)
	return count, err
}

// CountRemotePresent returns the number of filesync records currently known
// to exist on the remote server (a remote_path was recorded and it hasn't
// been flagged as remote-deleted).
func (h *SqliteHelper) CountRemotePresent() (int, error) {
	var count int
	err := h.DB.QueryRow(`SELECT COUNT(*) FROM filesync WHERE remote_path != '' AND remote_deleted = 0`).Scan(&count)
	return count, err
}

// CountPendingChanges returns how many filesync records still need
// reconciling by LiveCacheSync: items only present on one side
// (remote_only/local_only), out-of-sync content, or a deletion on one side
// not yet propagated to the other (see LiveCacheSync's QueryRemoteDeleted /
// QueryLocalDeleted for the matching conditions). It backs the GUI's
// background sync job: LiveCacheSync only runs when this is non-zero.
func (h *SqliteHelper) CountPendingChanges() (int, error) {
	var count int
	err := h.DB.QueryRow(`
		SELECT COUNT(*) FROM filesync WHERE
			sync_status IN ('remote_only', 'local_only', 'out_of_sync')
			OR (remote_deleted = 1 AND local_deleted = 0 AND local_path != '')
			OR (local_deleted = 1 AND remote_deleted = 0 AND node_id != '' AND remote_path != '')
	`).Scan(&count)
	return count, err
}
