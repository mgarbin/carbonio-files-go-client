package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigPopulatesAllFields verifies that a well-formed YAML config
// file is fully unmarshaled into the nested Config struct: every Main,
// Sync and Logging field must round-trip exactly, and AuthToken (a
// *string) must end up as a non-nil pointer to the configured value when
// the key is present in the YAML.
func TestLoadConfigPopulatesAllFields(t *testing.T) {
	const yamlContent = `
Main:
  endpoint: https://files.example.com
  username: alice
  password: s3cr3t
  authToken: tok-123
  filesLocalFolder: /home/alice/files
Sync:
  deleteRemoteNode: delete
Logging:
  level: debug
  format: json
  output: both
  path: /var/log/carbonio-files-go-client.log
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(%q) returned unexpected error: %v", path, err)
	}
	if cfg == nil {
		t.Fatalf("LoadConfig(%q) returned nil Config with nil error", path)
	}

	if cfg.Main.Endpoint != "https://files.example.com" {
		t.Errorf("Main.Endpoint = %q, want %q", cfg.Main.Endpoint, "https://files.example.com")
	}
	if cfg.Main.Username != "alice" {
		t.Errorf("Main.Username = %q, want %q", cfg.Main.Username, "alice")
	}
	if cfg.Main.Password != "s3cr3t" {
		t.Errorf("Main.Password = %q, want %q", cfg.Main.Password, "s3cr3t")
	}
	if cfg.Main.AuthToken == nil {
		t.Fatalf("Main.AuthToken = nil, want non-nil pointer to %q", "tok-123")
	}
	if *cfg.Main.AuthToken != "tok-123" {
		t.Errorf("*Main.AuthToken = %q, want %q", *cfg.Main.AuthToken, "tok-123")
	}
	if cfg.Main.FilesFolder != "/home/alice/files" {
		t.Errorf("Main.FilesFolder = %q, want %q", cfg.Main.FilesFolder, "/home/alice/files")
	}

	if cfg.Sync.DeleteRemoteNode != "delete" {
		t.Errorf("Sync.DeleteRemoteNode = %q, want %q", cfg.Sync.DeleteRemoteNode, "delete")
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging.Format = %q, want %q", cfg.Logging.Format, "json")
	}
	if cfg.Logging.Output != "both" {
		t.Errorf("Logging.Output = %q, want %q", cfg.Logging.Output, "both")
	}
	if cfg.Logging.Path != "/var/log/carbonio-files-go-client.log" {
		t.Errorf("Logging.Path = %q, want %q", cfg.Logging.Path, "/var/log/carbonio-files-go-client.log")
	}
}

// TestLoadConfigAuthTokenOmittedIsNil verifies that when the optional
// authToken key is absent from the YAML entirely, Main.AuthToken stays a
// nil pointer rather than being defaulted to a pointer-to-empty-string.
// This matters because callers use a nil AuthToken to decide whether to
// fall back to username/password authentication.
func TestLoadConfigAuthTokenOmittedIsNil(t *testing.T) {
	const yamlContent = `
Main:
  endpoint: https://files.example.com
  username: alice
  password: s3cr3t
  filesLocalFolder: /home/alice/files
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(%q) returned unexpected error: %v", path, err)
	}
	if cfg.Main.AuthToken != nil {
		t.Errorf("Main.AuthToken = %q, want nil when authToken key is omitted", *cfg.Main.AuthToken)
	}
}

// TestLoadConfigNonexistentFile verifies that LoadConfig surfaces the
// underlying os.ReadFile error (rather than panicking or silently
// returning a zero-value Config) when the target path does not exist.
func TestLoadConfigNonexistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.yml")

	cfg, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("LoadConfig(%q) returned nil error, want non-nil error for missing file", path)
	}
	if cfg != nil {
		t.Errorf("LoadConfig(%q) returned non-nil Config %+v, want nil on error", path, cfg)
	}
}

// TestLoadConfigInvalidYAML verifies that LoadConfig propagates a YAML
// parse error instead of returning a partially populated Config when the
// file content is not valid YAML.
func TestLoadConfigInvalidYAML(t *testing.T) {
	const invalidYAML = "Main: [this is not: valid yaml\n  - broken"

	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yml")
	if err := os.WriteFile(path, []byte(invalidYAML), 0o600); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("LoadConfig(%q) returned nil error, want non-nil error for invalid YAML", path)
	}
	if cfg != nil {
		t.Errorf("LoadConfig(%q) returned non-nil Config %+v, want nil on error", path, cfg)
	}
}
