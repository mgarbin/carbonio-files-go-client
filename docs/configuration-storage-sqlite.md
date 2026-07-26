[← Back to README](../README.md)

## Configuration storage (SQLite)

In addition to `config.yaml`, the same `Main` configuration can be persisted in
the SQLite database managed by `pkg/sqlite` (`sqlitecache.SqliteHelper`), in a
singleton `config` table (`id` is always `1`). `Password` and `AuthToken` are
encrypted at rest with AES-256-GCM before being written; every other field
(`endpoint`, `username`, `files_local_folder`, `log_level`, `log_format`,
`log_output`, `log_path`, `sync_enabled`, `sync_interval_minutes`,
`delete_remote_node`) is stored as plain text. This is
exactly the mechanism both the [CLI](usage.md) (`file_sync_cache.db`) and
the [desktop GUI](desktop-gui.md) (`gui-config.db`) use to remember
your login across runs/launches and to cache the `ZM_AUTH_TOKEN`.

`AuthToken` specifically is managed by `carbonio.Session`
(`pkg/carbonio/session.go`), shared by the CLI and the GUI:

- `Session.Login()` reads the stored token and calls
  `HTTPAuthenticator.ValidateToken` (`GET /zx/auth/v2/myself` with the token
  as the `ZM_AUTH_TOKEN` cookie) before trusting it. A `200` means the
  token is reused as-is - no password round-trip. A `401` (or no stored
  token) means the server no longer accepts it, so `Login()` falls back to
  a full `username`/`password` login via `CarbonioZxAuth` and persists the
  fresh token for the next call to reuse.
- `Session.Reauthenticate()` skips the cached-token check and always
  performs a fresh password login (used by the GUI's explicit "Login" and
  "Test connection" actions, where the whole point is to check the exact
  credentials just typed).

The AES-256 key is generated on first use and stored next to the database as
`<dbPath>.key` with `0600` permissions, kept separate from the `.db` file so a
copy of the database alone cannot be decrypted.

```go
h, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))

err = h.CreateConfig(sqlitecache.ConfigRecord{
    Endpoint:  "mail.example.com",
    Username:  "myemail",
    Password:  "mypassword",
    AuthToken: "", // optional
})

cfg, err := h.GetConfig()      // nil, nil if none was saved yet
err = h.UpdateConfig(cfg2)     // ErrConfigNotFound if none exists
err = h.UpsertConfig(cfg3)     // create or update
err = h.DeleteConfig()         // ErrConfigNotFound if none exists
```
