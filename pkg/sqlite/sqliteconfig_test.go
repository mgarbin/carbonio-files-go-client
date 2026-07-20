package sqlitecache

import (
	"bytes"
	"errors"
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
		Endpoint:         "mail.example.com",
		Username:         "myemail",
		Password:         "mypassword",
		AuthToken:        authToken,
		FilesLocalFolder: "./files",
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
	if got.Endpoint != in.Endpoint || got.Username != in.Username || got.Password != in.Password || got.AuthToken != in.AuthToken || got.FilesLocalFolder != in.FilesLocalFolder {
		t.Fatalf("GetConfig() = %+v, want fields matching %+v", got, in)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("GetConfig() timestamps not set: %+v", got)
	}

	// Update, including clearing the optional AuthToken.
	updated := ConfigRecord{
		Endpoint:         "mail2.example.com",
		Username:         "otheremail",
		Password:         "newpassword",
		AuthToken:        "",
		FilesLocalFolder: "./other-files",
	}
	if err := h.UpdateConfig(updated); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	got, err = h.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig() after update error = %v", err)
	}
	if got.Endpoint != updated.Endpoint || got.Password != updated.Password || got.AuthToken != "" {
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
