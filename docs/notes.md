[← Back to README](../README.md)

## Notes

- The Carbonio server TLS certificate is not verified (`InsecureSkipVerify`). This allows connections to servers using self-signed certificates.
- Downloads are performed with a maximum of one concurrent operation.
- File integrity is verified using SHA-384 digests.
- The SQLite cache file (`./file_sync_cache.db`) is created automatically in the current directory when using cache-based sync operations.
- The local download/sync directory (`./files/`) is created automatically when needed.
- The configuration table's AES-256 key (`<dbPath>.key`, e.g. `./file_sync_cache.db.key`) holds the material needed to decrypt stored credentials; treat it like a secret and never commit it (both `*.db` and `*.db.key` are gitignored).
- The desktop GUI's credential database (`gui-config.db` under the OS config directory) and its `.key` sibling are as sensitive as the CLI's `./file_sync_cache.db`/`.key` and are never written to the project directory.
- Cached-token reuse (both CLI and GUI) is validated with `GET /zx/auth/v2/myself`, the same versioned endpoint `/zx/auth/v2/login` lives under. Per the server implementation (`carbonio-auth`'s `AuthorizedApiHandler`/`MyselfAuthHandler`, and `AccountService.getToken` which raises `ServiceException.AUTH_EXPIRED` for an expired or deregistered token), every reason a `ZM_AUTH_TOKEN` stops being usable maps to a bare HTTP 401 with no body — that is the only status treated as "token no longer suitable, re-authenticate"; any other non-200 (network error, 5xx, ...) is surfaced as an error instead and does not trigger a password fallback.
