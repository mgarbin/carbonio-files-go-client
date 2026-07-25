[← Back to README](../README.md)

## Configuration

Create a `config.yaml` file in the directory where you run the client:

```yaml
Main:
  endpoint: "mail.example.com"   # Carbonio server hostname or IP
  username: "myuser"             # Carbonio account username
  password: "mypassword"         # Carbonio account password
#  AuthToken: "ZM_AUTH_TOKEN"    # Optional: pre-computed auth token (skips login)
#  filesLocalFolder: "./files"   # Optional: by default it create the folder "files" where you are running carbonio-files-go-client

Sync:
#  deleteRemoteNode: "trash"  # "trash" (default) moves the remote node to trash, "delete" removes it permanently

Logging:
#  level: "info"      # trace, debug, info, warn, error, fatal, panic, disabled (default: info)
#  format: "console"  # "console" (human-readable, colorized) or "json" (default: console)
#  output: "console"  # "console", "file" or "both" (default: console)
#  path: "<home>/.carbonio_files_sync/carbonio-files-go-client.log"  # log file path, used when output is "file" or "both"
```

When `AuthToken` is set in `config.yaml`, it is used verbatim and neither the
cached-token store nor the login step ever runs - use this only for a
token you manage yourself. Leave it unset (the default) to get automatic
token caching instead: the first run logs in with `username`/`password` and
saves the resulting `ZM_AUTH_TOKEN` encrypted at rest in
`<home>/.carbonio_files_sync/file_sync_cache.db` (see [Configuration storage (SQLite)](configuration-storage-sqlite.md));
every following run reuses that token - skipping the password login
entirely - for as long as the server keeps accepting it, and transparently
re-authenticates with `username`/`password` (persisting the refreshed
token) the moment the server reports it no longer is.

Every `Logging` field is optional and overridable from the command line (see
[Logging](logging.md) below); the `Logging` block itself may be omitted entirely
to use the built-in defaults (info level, console format, console output).

`Sync.deleteRemoteNode` controls how `-liveCacheSync`/`-fullCacheSync`
propagate a local deletion to the remote node: `"trash"` (the default, used
when the `Sync` block or this field is omitted) moves it to trash,
`"delete"` removes it permanently. The desktop GUI exposes the same choice
from Preferences > Synchronization, persisted in the SQLite config instead
(see [Configuration storage (SQLite)](configuration-storage-sqlite.md)).
