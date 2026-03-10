package sqlitecache

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

type SqliteHelper struct {
	DB *sql.DB
}

// FileSyncRecord represents a row in the filesync table.
type FileSyncRecord struct {
	ID                 int64
	NodeID             string
	ParentID           string
	RemotePath         string
	RemotePathHash     string
	LocalPath          string
	LocalPathHash      string
	IsDirectory        bool
	RemoteLastModified string
	LocalLastModified  string
	RemoteSize         int64
	LocalSize          int64
	RemoteDigest       string
	LocalDigest        string
	SyncStatus         string
	LastSynced         string
	LocalDeleted       int
	RemoteDeleted      int
}

// NewSqliteHelper crea/apre il database e assicura che la tabella filesync esista.
func NewSqliteHelper(dbPath string) (*SqliteHelper, error) {
	// Crea file vuoto se non esiste
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		file, err := os.Create(dbPath)
		if err != nil {
			return nil, fmt.Errorf("impossibile creare il file db: %w", err)
		}
		file.Close()
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("impossibile aprire il db: %w", err)
	}

	// Check for corruption
	row := db.QueryRow("PRAGMA integrity_check;")
	var integrityResult string
	if err := row.Scan(&integrityResult); err != nil {
		db.Close()
		return nil, fmt.Errorf("error running integrity_check: %w", err)
	}
	if integrityResult != "ok" {
		db.Close()
		return nil, fmt.Errorf("database %s is corrupted: %s", dbPath, integrityResult)
	}

	// Assicura che la tabella filesync esista
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS filesync (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		node_id TEXT,
		parent_id TEXT,
		remote_path TEXT NOT NULL,
		remote_path_hash TEXT,
		local_path TEXT NOT NULL,
		local_path_hash TEXT,
		is_directory BOOLEAN NOT NULL,
		remote_last_modified TEXT,
		local_last_modified TEXT,
		remote_size INTEGER,
		local_size INTEGER,
		remote_digest TEXT,
		local_digest TEXT,
		sync_status TEXT,
		last_synced TEXT,
		local_deleted INTEGER NOT NULL DEFAULT 0,
		remote_deleted INTEGER NOT NULL DEFAULT 0
	);`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("errore creando la tabella filesync: %w", err)
	}

	createIndexesSQL := []string{
		`CREATE INDEX IF NOT EXISTS idx_filesync_remote_path_dir_del ON filesync (remote_path, is_directory, remote_deleted);`,
		`CREATE INDEX IF NOT EXISTS idx_filesync_local_path_dir_del ON filesync (local_path, is_directory, local_deleted);`,
	}
	for _, indexSQL := range createIndexesSQL {
		if _, err = db.Exec(indexSQL); err != nil {
			return nil, fmt.Errorf("errore creando un indice su filesync: %w", err)
		}
	}

	return &SqliteHelper{DB: db}, nil
}

// InsertFileSync inserisce un nuovo record nella tabella filesync.
func (h *SqliteHelper) InsertFileSync(
	nodeID, parentID, remotePath, remotePathHash, localPath, localPathHash string,
	isDirectory bool,
	remoteLastModified, localLastModified string,
	remoteSize, localSize int64,
	remoteDigest, localDigest, syncStatus, lastSynced string,
	localDeleted, remoteDeleted int,
) (int64, error) {
	stmt := `
        INSERT INTO filesync (
            node_id, parent_id, remote_path, remote_path_hash, local_path, local_path_hash, is_directory, remote_last_modified, local_last_modified,
            remote_size, local_size, remote_digest, local_digest, sync_status, last_synced, local_deleted, remote_deleted
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := h.DB.Exec(stmt,
		nodeID, parentID, remotePath, remotePathHash, localPath, localPathHash, isDirectory, remoteLastModified, localLastModified,
		remoteSize, localSize, remoteDigest, localDigest, syncStatus, lastSynced, localDeleted, remoteDeleted)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

//	err := helper.UpdateFileSync("node_id", "my-node-uuid", map[string]interface{}{
//		"sync_status": "pending_upload",
//	})
//
// UpdateFileSync aggiorna solo i campi specificati per la riga selezionata tramite id, node_id o local_digest
func (h *SqliteHelper) UpdateFileSync(selectorField string, selectorValue interface{}, fields map[string]interface{}) error {
	if len(fields) == 0 {
		return nil // niente da aggiornare
	}
	// Controlla il campo selettore
	validSelectors := map[string]bool{"id": true, "node_id": true, "local_digest": true}
	if !validSelectors[selectorField] {
		return fmt.Errorf("selettore non valido: %s", selectorField)
	}

	query := "UPDATE filesync SET "
	args := []interface{}{}
	i := 0
	for col, val := range fields {
		if i > 0 {
			query += ", "
		}
		query += col + " = ?"
		args = append(args, val)
		i++
	}
	query += " WHERE " + selectorField + " = ?"
	args = append(args, selectorValue)

	_, err := h.DB.Exec(query, args...)
	return err
}

// DeleteAllAndResetAutoIncrement elimina tutte le righe dalla tabella filesync e resetta l'autoincrement dell'id.
func (h *SqliteHelper) DeleteAllAndResetAutoIncrement() error {
	tx, err := h.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	_, err = tx.Exec("DELETE FROM filesync;")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM sqlite_sequence WHERE name='filesync';")
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Chiudi la connessione
func (h *SqliteHelper) Close() error {
	return h.DB.Close()
}

const selectAllColumns = `SELECT id, node_id, parent_id, remote_path, remote_path_hash, local_path, local_path_hash,
	is_directory, remote_last_modified, local_last_modified, remote_size, local_size,
	remote_digest, local_digest, sync_status, last_synced, local_deleted, remote_deleted FROM filesync`

// QueryAll returns all records from the filesync table.
func (h *SqliteHelper) QueryAll() ([]FileSyncRecord, error) {
	rows, err := h.DB.Query(selectAllColumns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileSyncRows(rows)
}

// QueryBySyncStatus returns all records with the given sync_status value.
func (h *SqliteHelper) QueryBySyncStatus(status string) ([]FileSyncRecord, error) {
	rows, err := h.DB.Query(selectAllColumns+` WHERE sync_status = ?`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFileSyncRows(rows)
}

// QueryFolderByPath returns the first folder record whose remote_path or local_path matches
// the given folderPath and that has a non-empty node_id. It returns nil when no such record exists.
func (h *SqliteHelper) QueryFolderByPath(folderPath string) (*FileSyncRecord, error) {
	rows, err := h.DB.Query(
		selectAllColumns+` WHERE (remote_path = ? OR local_path = ?) AND is_directory = 1 AND node_id != '' AND local_deleted = 0 AND remote_deleted = 0 LIMIT 1`,
		folderPath, folderPath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records, err := scanFileSyncRows(rows)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func scanFileSyncRows(rows *sql.Rows) ([]FileSyncRecord, error) {
	var records []FileSyncRecord
	for rows.Next() {
		var rec FileSyncRecord
		var isDirInt int
		err := rows.Scan(
			&rec.ID, &rec.NodeID, &rec.ParentID,
			&rec.RemotePath, &rec.RemotePathHash,
			&rec.LocalPath, &rec.LocalPathHash,
			&isDirInt,
			&rec.RemoteLastModified, &rec.LocalLastModified,
			&rec.RemoteSize, &rec.LocalSize,
			&rec.RemoteDigest, &rec.LocalDigest,
			&rec.SyncStatus, &rec.LastSynced,
			&rec.LocalDeleted, &rec.RemoteDeleted,
		)
		if err != nil {
			return nil, err
		}
		rec.IsDirectory = isDirInt != 0
		records = append(records, rec)
	}
	return records, rows.Err()
}
