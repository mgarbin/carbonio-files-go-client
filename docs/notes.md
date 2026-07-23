[← Back to README](../README.md)

## Notes

- The Carbonio server TLS certificate is not verified (`InsecureSkipVerify`). This allows connections to servers using self-signed certificates.
- Downloads are performed with a maximum of one concurrent operation.
- File integrity is verified using SHA-384 digests.
- The SQLite cache file (`./file_sync_cache.db`) is created automatically in the current directory when using cache-based sync operations.
- The local download/sync directory (`./files/`) is created automatically when needed.
- The configuration table's AES-256 key (`<dbPath>.key`, e.g. `./file_sync_cache.db.key`) holds the material needed to decrypt stored credentials; treat it like a secret and never commit it (both `*.db` and `*.db.key` are gitignored).
- The desktop GUI's credential database (`gui-config.db` under the OS config directory) and its `.key` sibling are as sensitive as the CLI's `./file_sync_cache.db`/`.key` and are never written to the project directory.
