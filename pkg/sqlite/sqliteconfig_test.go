package sqlitecache

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func newTestHelper(t *testing.T) (*SqliteHelper, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	h, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	t.Cleanup(func() { h.Close() })
	return h, dbPath
}

func TestConfigCRUD(t *testing.T) {
	h, _ := newTestHelper(t)

	if cfg, err := h.GetConfig(); err != nil || cfg != nil {
		t.Fatalf("GetConfig() on empty table = (%v, %v), want (nil, nil)", cfg, err)
	}

	authToken := "secret-auth-token"
	in := ConfigRecord{
		Endpoint:            "mail.example.com",
		Username:            "myemail",
		Password:            "mypassword",
		AuthToken:           authToken,
		FilesLocalFolder:    "./files",
		LogLevel:            "debug",
		LogFormat:           "json",
		LogOutput:           "both",
		LogPath:             "/tmp/carbonio-test.log",
		SyncEnabled:         true,
		SyncIntervalMinutes: 15,
	}
	if err := h.CreateConfig(in); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}

	// Creating a second time must fail: the table is a singleton.
	if err := h.CreateConfig(in); !errors.Is(err, ErrConfigExists) {
		t.Fatalf("CreateConfig() second call error = %v, want ErrConfigExists", err)
	}

	got, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetConfig() = nil, want a record")
	}
	if got.Endpoint != in.Endpoint || got.Username != in.Username || got.Password != in.Password || got.AuthToken != in.AuthToken || got.FilesLocalFolder != in.FilesLocalFolder ||
		got.LogLevel != in.LogLevel || got.LogFormat != in.LogFormat || got.LogOutput != in.LogOutput || got.LogPath != in.LogPath || got.SyncEnabled != in.SyncEnabled ||
		got.SyncIntervalMinutes != in.SyncIntervalMinutes {
		t.Fatalf("GetConfig() = %+v, want fields matching %+v", got, in)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("GetConfig() timestamps not set: %+v", got)
	}

	// Update, including clearing the optional AuthToken.
	updated := ConfigRecord{
		Endpoint:            "mail2.example.com",
		Username:            "otheremail",
		Password:            "newpassword",
		AuthToken:           "",
		FilesLocalFolder:    "./other-files",
		LogLevel:            "warn",
		LogFormat:           "console",
		LogOutput:           "file",
		LogPath:             "/tmp/carbonio-other.log",
		SyncEnabled:         false,
		SyncIntervalMinutes: 60,
	}
	if err := h.UpdateConfig(updated); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	got, err = h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after update error = %v", err)
	}
	if got.Endpoint != updated.Endpoint || got.Password != updated.Password || got.AuthToken != "" ||
		got.LogLevel != updated.LogLevel || got.LogFormat != updated.LogFormat || got.LogOutput != updated.LogOutput || got.LogPath != updated.LogPath || got.SyncEnabled != updated.SyncEnabled ||
		got.SyncIntervalMinutes != updated.SyncIntervalMinutes {
		t.Fatalf("GetConfig() after update = %+v, want fields matching %+v", got, updated)
	}

	if err := h.DeleteConfig(); err != nil {
		t.Fatalf("DeleteConfig() error = %v", err)
	}
	if cfg, err := h.GetConfig(); err != nil || cfg != nil {
		t.Fatalf("GetConfig() after delete = (%v, %v), want (nil, nil)", cfg, err)
	}
	if err := h.DeleteConfig(); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("DeleteConfig() on empty table error = %v, want ErrConfigNotFound", err)
	}
	if err := h.UpdateConfig(updated); !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("UpdateConfig() on empty table error = %v, want ErrConfigNotFound", err)
	}
}

func TestUpsertConfig(t *testing.T) {
	h, _ := newTestHelper(t)

	cfg := ConfigRecord{Endpoint: "a", Username: "u", Password: "p"}
	if err := h.UpsertConfig(cfg); err != nil {
		t.Fatalf("UpsertConfig() create error = %v", err)
	}
	cfg.Endpoint = "b"
	if err := h.UpsertConfig(cfg); err != nil {
		t.Fatalf("UpsertConfig() update error = %v", err)
	}
	got, err := h.GetConfig()
	if err != nil || got == nil || got.Endpoint != "b" {
		t.Fatalf("GetConfig() after upsert = (%+v, %v), want Endpoint=b", got, err)
	}
}

// TestConfigSecretsAreEncryptedAtRest opens the raw db file and confirms the
// plaintext password/authToken never appear on disk, while non-secret fields
// (endpoint, username) are stored as plain, queryable text.
func TestConfigSecretsAreEncryptedAtRest(t *testing.T) {
	h, dbPath := newTestHelper(t)

	cfg := ConfigRecord{
		Endpoint:         "mail.example.com",
		Username:         "plaintext-username",
		Password:         "super-secret-password",
		AuthToken:        "super-secret-token",
		FilesLocalFolder: "./files",
	}
	if err := h.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}
	h.Close()

	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", dbPath, err)
	}
	if bytes.Contains(raw, []byte(cfg.Password)) {
		t.Fatal("raw db file contains the plaintext password")
	}
	if bytes.Contains(raw, []byte(cfg.AuthToken)) {
		t.Fatal("raw db file contains the plaintext authToken")
	}
	if !bytes.Contains(raw, []byte(cfg.Username)) {
		t.Fatal("raw db file should still contain the plaintext (non-secret) username")
	}

	if _, err := os.Stat(dbPath + ".key"); err != nil {
		t.Fatalf("expected config key file at %s.key: %v", dbPath, err)
	}
	info, err := os.Stat(dbPath + ".key")
	if err != nil {
		t.Fatalf("os.Stat(key) error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config key file perm = %o, want 0600", perm)
	}
}

// TestConfigKeyPersistsAcrossReopen ensures the same key (and therefore the
// ability to decrypt) survives closing and reopening the helper.
func TestConfigKeyPersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	h1, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	cfg := ConfigRecord{Endpoint: "e", Username: "u", Password: "p", AuthToken: "t"}
	if err := h1.CreateConfig(cfg); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}
	h1.Close()

	h2, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("second NewSqliteHelper() error = %v", err)
	}
	defer h2.Close()

	got, err := h2.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after reopen error = %v", err)
	}
	if got == nil || got.Password != cfg.Password || got.AuthToken != cfg.AuthToken {
		t.Fatalf("GetConfig() after reopen = %+v, want matching secrets", got)
	}
}

// TestConfigTableMigrationAddsLoggingColumns verifies that opening a
// database created before the log_* columns existed transparently adds
// them (via addColumnIfMissing) without touching the pre-existing row, and
// that the new columns are then fully usable.
func TestConfigTableMigrationAddsLoggingColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// Create a fully-migrated database first (so the config key file and a
	// properly encrypted row exist), then drop the log_* columns to
	// simulate a database created before they existed.
	h0, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	legacyCfg := ConfigRecord{
		Endpoint:         "legacy.example.com",
		Username:         "legacyuser",
		Password:         "legacy-password",
		FilesLocalFolder: "./files",
	}
	if err := h0.CreateConfig(legacyCfg); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}
	for _, col := range []string{"log_level", "log_format", "log_output", "log_path"} {
		if _, err := h0.DB.Exec(fmt.Sprintf("ALTER TABLE config DROP COLUMN %s", col)); err != nil {
			t.Fatalf("dropping column %s to simulate legacy schema failed: %v", col, err)
		}
	}
	if err := h0.Close(); err != nil {
		t.Fatalf("h0.Close() error = %v", err)
	}

	// Opening it again through SqliteHelper must migrate the table in place.
	h, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() on legacy db error = %v", err)
	}
	defer h.Close()

	var logLevel, logFormat, logOutput, logPath string
	row := h.DB.QueryRow(`SELECT log_level, log_format, log_output, log_path FROM config WHERE id = 1`)
	if err := row.Scan(&logLevel, &logFormat, &logOutput, &logPath); err != nil {
		t.Fatalf("querying migrated logging columns failed: %v", err)
	}
	if logLevel != "" || logFormat != "" || logOutput != "" || logPath != "" {
		t.Fatalf("migrated logging columns = (%q, %q, %q, %q), want all empty", logLevel, logFormat, logOutput, logPath)
	}

	// The pre-existing row must have survived the migration untouched.
	got, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after migration error = %v", err)
	}
	if got == nil || got.Endpoint != legacyCfg.Endpoint || got.Username != legacyCfg.Username || got.Password != legacyCfg.Password {
		t.Fatalf("GetConfig() after migration = %+v, want legacy row preserved", got)
	}

	// The newly-added logging columns must be fully usable going forward.
	got.LogLevel = "debug"
	got.LogFormat = "json"
	got.LogOutput = "both"
	got.LogPath = "/tmp/carbonio.log"
	if err := h.UpdateConfig(*got); err != nil {
		t.Fatalf("UpdateConfig() after migration error = %v", err)
	}
	got2, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after logging update error = %v", err)
	}
	if got2.LogLevel != "debug" || got2.LogFormat != "json" || got2.LogOutput != "both" || got2.LogPath != "/tmp/carbonio.log" {
		t.Fatalf("GetConfig() after logging update = %+v, want logging fields set", got2)
	}

	// Re-running the migration on an already-migrated table must be a no-op.
	if err := ensureConfigTable(h.DB); err != nil {
		t.Fatalf("re-running ensureConfigTable() error = %v", err)
	}
}

// TestConfigTableMigrationAddsSyncEnabledColumn mirrors
// TestConfigTableMigrationAddsLoggingColumns for the sync_enabled column:
// opening a database created before it existed must transparently add it
// (defaulting to disabled) without touching the pre-existing row, and the
// column must then be fully usable.
func TestConfigTableMigrationAddsSyncEnabledColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-sync-enabled.db")

	h0, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	legacyCfg := ConfigRecord{
		Endpoint:         "legacy.example.com",
		Username:         "legacyuser",
		Password:         "legacy-password",
		FilesLocalFolder: "./files",
	}
	if err := h0.CreateConfig(legacyCfg); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}
	if _, err := h0.DB.Exec(`ALTER TABLE config DROP COLUMN sync_enabled`); err != nil {
		t.Fatalf("dropping column sync_enabled to simulate legacy schema failed: %v", err)
	}
	if err := h0.Close(); err != nil {
		t.Fatalf("h0.Close() error = %v", err)
	}

	// Opening it again through SqliteHelper must migrate the table in place.
	h, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() on legacy db error = %v", err)
	}
	defer h.Close()

	var syncEnabled int
	row := h.DB.QueryRow(`SELECT sync_enabled FROM config WHERE id = 1`)
	if err := row.Scan(&syncEnabled); err != nil {
		t.Fatalf("querying migrated sync_enabled column failed: %v", err)
	}
	if syncEnabled != 0 {
		t.Fatalf("migrated sync_enabled column = %d, want 0 (disabled by default)", syncEnabled)
	}

	// The pre-existing row must have survived the migration untouched.
	got, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after migration error = %v", err)
	}
	if got == nil || got.Endpoint != legacyCfg.Endpoint || got.SyncEnabled {
		t.Fatalf("GetConfig() after migration = %+v, want legacy row preserved with SyncEnabled=false", got)
	}

	// The newly-added column must be fully usable going forward.
	got.SyncEnabled = true
	if err := h.UpdateConfig(*got); err != nil {
		t.Fatalf("UpdateConfig() after migration error = %v", err)
	}
	got2, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after sync_enabled update error = %v", err)
	}
	if !got2.SyncEnabled {
		t.Fatalf("GetConfig() after sync_enabled update = %+v, want SyncEnabled=true", got2)
	}
}

// TestConfigTableMigrationAddsSyncIntervalColumn mirrors
// TestConfigTableMigrationAddsSyncEnabledColumn for the
// sync_interval_minutes column: opening a database created before it
// existed must transparently add it (defaulting to 0, i.e. "use the
// built-in default" - see ConfigRecord.SyncIntervalMinutes) without
// touching the pre-existing row, and the column must then be fully usable.
func TestConfigTableMigrationAddsSyncIntervalColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-sync-interval.db")

	h0, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() error = %v", err)
	}
	legacyCfg := ConfigRecord{
		Endpoint:         "legacy.example.com",
		Username:         "legacyuser",
		Password:         "legacy-password",
		FilesLocalFolder: "./files",
	}
	if err := h0.CreateConfig(legacyCfg); err != nil {
		t.Fatalf("CreateConfig() error = %v", err)
	}
	if _, err := h0.DB.Exec(`ALTER TABLE config DROP COLUMN sync_interval_minutes`); err != nil {
		t.Fatalf("dropping column sync_interval_minutes to simulate legacy schema failed: %v", err)
	}
	if err := h0.Close(); err != nil {
		t.Fatalf("h0.Close() error = %v", err)
	}

	// Opening it again through SqliteHelper must migrate the table in place.
	h, err := NewSqliteHelper(dbPath)
	if err != nil {
		t.Fatalf("NewSqliteHelper() on legacy db error = %v", err)
	}
	defer h.Close()

	var syncIntervalMinutes int
	row := h.DB.QueryRow(`SELECT sync_interval_minutes FROM config WHERE id = 1`)
	if err := row.Scan(&syncIntervalMinutes); err != nil {
		t.Fatalf("querying migrated sync_interval_minutes column failed: %v", err)
	}
	if syncIntervalMinutes != 0 {
		t.Fatalf("migrated sync_interval_minutes column = %d, want 0 (unset)", syncIntervalMinutes)
	}

	// The pre-existing row must have survived the migration untouched.
	got, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after migration error = %v", err)
	}
	if got == nil || got.Endpoint != legacyCfg.Endpoint || got.SyncIntervalMinutes != 0 {
		t.Fatalf("GetConfig() after migration = %+v, want legacy row preserved with SyncIntervalMinutes=0", got)
	}

	// The newly-added column must be fully usable going forward.
	got.SyncIntervalMinutes = 30
	if err := h.UpdateConfig(*got); err != nil {
		t.Fatalf("UpdateConfig() after migration error = %v", err)
	}
	got2, err := h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after sync_interval_minutes update error = %v", err)
	}
	if got2.SyncIntervalMinutes != 30 {
		t.Fatalf("GetConfig() after sync_interval_minutes update = %+v, want SyncIntervalMinutes=30", got2)
	}
}
