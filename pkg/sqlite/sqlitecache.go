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
		deleted BOOLEAN NOT NULL DEFAULT 0
	);`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("errore creando la tabella filesync: %w", err)
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
	deleted bool,
) (int64, error) {
	stmt := `
        INSERT INTO filesync (
            node_id, parent_id, remote_path, remote_path_hash, local_path, local_path_hash, is_directory, remote_last_modified, local_last_modified,
            remote_size, local_size, remote_digest, local_digest, sync_status, last_synced, deleted
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := h.DB.Exec(stmt,
		nodeID, parentID, remotePath, remotePathHash, localPath, localPathHash, isDirectory, remoteLastModified, localLastModified,
		remoteSize, localSize, remoteDigest, localDigest, syncStatus, lastSynced, deleted)
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
