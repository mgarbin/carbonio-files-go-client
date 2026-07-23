[← Back to README](../README.md)

## Configuration storage (SQLite)

In addition to `config.yaml`, the same `Main` configuration can be persisted in
the SQLite database managed by `pkg/sqlite` (`sqlitecache.SqliteHelper`), in a
singleton `config` table (`id` is always `1`). `Password` and `AuthToken` are
encrypted at rest with AES-256-GCM before being written; every other field
(`endpoint`, `username`, `files_local_folder`, `log_level`, `log_format`,
`log_output`, `log_path`) is stored as plain text. This is
exactly the mechanism the [desktop GUI](desktop-gui.md) uses to remember your
login across launches (in its own `gui-config.db`, independent from the CLI's
`./file_sync_cache.db`).

The AES-256 key is generated on first use and stored next to the database as
`<dbPath>.key` with `0600` permissions, kept separate from the `.db` file so a
copy of the database alone cannot be decrypted.

```go
h, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")

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
