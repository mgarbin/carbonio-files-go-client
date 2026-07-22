package sqlitecache

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// configKeySize is the size, in bytes, of the AES-256 key used to encrypt
// sensitive configuration fields (password, authToken) at rest.
const configKeySize = 32

// ConfigRecord mirrors the "Main" section of config.yaml. Password and
// AuthToken are the only secret fields: they are stored encrypted in the
// config table. Every other field carries no secret material and is stored
// as plain text so it stays queryable/inspectable.
type ConfigRecord struct {
	Endpoint         string
	Username         string
	Password         string
	AuthToken        string // "" means "not set" (NULL in the db)
	FilesLocalFolder string
	// LogLevel, LogFormat, LogOutput and LogPath mirror pkg/logger.Config
	// (see pkg/config.LoggingConfig for the accepted values). They carry no
	// secret material and are stored as plain text. Empty means "use the
	// built-in default" (logger.Default()).
	LogLevel  string
	LogFormat string
	LogOutput string
	LogPath   string
	CreatedAt string
	UpdatedAt string
}

var (
	// ErrConfigExists is returned by CreateConfig when a configuration row already exists.
	ErrConfigExists = errors.New("configuration already exists: use UpdateConfig")
	// ErrConfigNotFound is returned by UpdateConfig/DeleteConfig when no configuration row exists.
	ErrConfigNotFound = errors.New("configuration not found: use CreateConfig")
)

// ensureConfigTable creates the "config" table: a singleton (id is always 1)
// that holds the same configuration used by config.yaml, with the sensitive
// fields already encrypted (see encryptSecret/decryptSecret).
func ensureConfigTable(db *sql.DB) error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		endpoint TEXT NOT NULL,
		username TEXT NOT NULL,
		password_enc BLOB NOT NULL,
		auth_token_enc BLOB,
		files_local_folder TEXT NOT NULL DEFAULT '',
		log_level TEXT NOT NULL DEFAULT '',
		log_format TEXT NOT NULL DEFAULT '',
		log_output TEXT NOT NULL DEFAULT '',
		log_path TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("error creating the config table: %w", err)
	}

	// Migrate databases created before the logging columns existed.
	for _, col := range []string{"log_level", "log_format", "log_output", "log_path"} {
		if err := addColumnIfMissing(db, "config", col, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing adds column to table with the given DDL (type +
// constraints) unless it already exists. SQLite's ALTER TABLE has no
// portable "ADD COLUMN IF NOT EXISTS", so existence is checked via
// PRAGMA table_info first.
func addColumnIfMissing(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("error inspecting table %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("error reading table_info(%s): %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading table_info(%s): %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, ddl)); err != nil {
		return fmt.Errorf("error adding column %s to %s: %w", column, table, err)
	}
	return nil
}

// loadOrCreateConfigKey reads the AES-256 key used to encrypt the sensitive
// configuration fields from "<dbPath>.key", generating it on first use. The
// file is saved with 0600 permissions and kept separate from the database:
// a copy of the .db file alone is not enough to reveal the credentials.
func loadOrCreateConfigKey(dbPath string) ([]byte, error) {
	keyPath := dbPath + ".key"

	data, err := os.ReadFile(keyPath)
	if err == nil {
		if len(data) != configKeySize {
			return nil, fmt.Errorf("invalid config key in %s: expected %d bytes, got %d", keyPath, configKeySize, len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("unable to read config key %s: %w", keyPath, err)
	}

	key := make([]byte, configKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("unable to generate config key: %w", err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("unable to save config key to %s: %w", keyPath, err)
	}
	return key, nil
}

// encryptSecret encrypts plaintext with AES-256-GCM using the configuration
// key. An empty string maps to nil (NULL column), so optional fields like
// AuthToken stay distinguishable from "set but empty".
func (h *SqliteHelper) encryptSecret(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	gcm, err := h.configGCM()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("unable to generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// decryptSecret is the inverse of encryptSecret. A nil/empty ciphertext decodes to "".
func (h *SqliteHelper) decryptSecret(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	gcm, err := h.configGCM()
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("config ciphertext is too short")
	}
	nonce, sealed := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("unable to decrypt config field (wrong key or corrupted data): %w", err)
	}
	return string(plaintext), nil
}

func (h *SqliteHelper) configGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(h.configKey)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize GCM: %w", err)
	}
	return gcm, nil
}

// CreateConfig inserts the configuration (id=1), encrypting password and
// authToken. Returns ErrConfigExists if a configuration already exists: use
// UpdateConfig to modify it.
func (h *SqliteHelper) CreateConfig(cfg ConfigRecord) error {
	passwordEnc, err := h.encryptSecret(cfg.Password)
	if err != nil {
		return err
	}
	authTokenEnc, err := h.encryptSecret(cfg.AuthToken)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = h.DB.Exec(
		`INSERT INTO config (id, endpoint, username, password_enc, auth_token_enc, files_local_folder, log_level, log_format, log_output, log_path, created_at, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cfg.Endpoint, cfg.Username, passwordEnc, authTokenEnc, cfg.FilesLocalFolder, cfg.LogLevel, cfg.LogFormat, cfg.LogOutput, cfg.LogPath, now, now,
	)
	if err != nil {
		if isConstraintErr(err) {
			return ErrConfigExists
		}
		return fmt.Errorf("error inserting configuration: %w", err)
	}
	return nil
}

// GetConfig reads the saved configuration, decrypting password and
// authToken. Returns (nil, nil) if no configuration has been saved yet.
func (h *SqliteHelper) GetConfig() (*ConfigRecord, error) {
	row := h.DB.QueryRow(
		`SELECT endpoint, username, password_enc, auth_token_enc, files_local_folder, log_level, log_format, log_output, log_path, created_at, updated_at
		 FROM config WHERE id = 1`,
	)

	var cfg ConfigRecord
	var passwordEnc, authTokenEnc []byte
	err := row.Scan(&cfg.Endpoint, &cfg.Username, &passwordEnc, &authTokenEnc, &cfg.FilesLocalFolder, &cfg.LogLevel, &cfg.LogFormat, &cfg.LogOutput, &cfg.LogPath, &cfg.CreatedAt, &cfg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error reading configuration: %w", err)
	}

	if cfg.Password, err = h.decryptSecret(passwordEnc); err != nil {
		return nil, err
	}
	if cfg.AuthToken, err = h.decryptSecret(authTokenEnc); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig replaces the existing configuration, encrypting password and
// authToken. Returns ErrConfigNotFound if there is no row to update: use
// CreateConfig for the first write.
func (h *SqliteHelper) UpdateConfig(cfg ConfigRecord) error {
	passwordEnc, err := h.encryptSecret(cfg.Password)
	if err != nil {
		return err
	}
	authTokenEnc, err := h.encryptSecret(cfg.AuthToken)
	if err != nil {
		return err
	}

	res, err := h.DB.Exec(
		`UPDATE config SET endpoint = ?, username = ?, password_enc = ?, auth_token_enc = ?, files_local_folder = ?, log_level = ?, log_format = ?, log_output = ?, log_path = ?, updated_at = ?
		 WHERE id = 1`,
		cfg.Endpoint, cfg.Username, passwordEnc, authTokenEnc, cfg.FilesLocalFolder, cfg.LogLevel, cfg.LogFormat, cfg.LogOutput, cfg.LogPath, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("error updating configuration: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error verifying configuration update: %w", err)
	}
	if affected == 0 {
		return ErrConfigNotFound
	}
	return nil
}

// UpsertConfig saves the configuration: creates it if absent, otherwise updates it.
func (h *SqliteHelper) UpsertConfig(cfg ConfigRecord) error {
	err := h.CreateConfig(cfg)
	if errors.Is(err, ErrConfigExists) {
		return h.UpdateConfig(cfg)
	}
	return err
}

// DeleteConfig removes the saved configuration. Returns ErrConfigNotFound if
// there was none to delete.
func (h *SqliteHelper) DeleteConfig() error {
	res, err := h.DB.Exec(`DELETE FROM config WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("error deleting configuration: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error verifying configuration deletion: %w", err)
	}
	if affected == 0 {
		return ErrConfigNotFound
	}
	return nil
}

// isConstraintErr reports whether err is a SQL constraint violation
// (PRIMARY KEY/UNIQUE/CHECK) raised by the sqlite driver.
func isConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "constraint failed")
}
