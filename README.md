# carbonio-files-go-client

Carbonio Files Client is a desktop app and command-line client for managing and synchronizing files with a [Carbonio](https://www.zextras.com/carbonio/) server. Run it with no arguments to open the desktop GUI, or pass `-cli` to use it as a scriptable command-line tool: uploading, downloading, listing, moving, trashing, deleting nodes, and performing bidirectional file synchronization backed by a local SQLite cache.

## License

This project is licensed under the [GNU Affero General Public License v3](COPYING).

## Requirements

- Go 1.24 or later
- Access to a Carbonio server
- To build the desktop GUI on Linux: GTK3 and WebKit2GTK 4.1 development packages (e.g. `gtk3` and `webkit2gtk-4.1` on Arch/Manjaro, `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` on Debian/Ubuntu)

## Installation

### Build from source

```bash
make build
```

This produces the `carbonio-files-client` binary in the project root (symbols stripped and optimized).

## Desktop GUI

Running the binary with **no arguments** opens the desktop GUI (built with [Wails](https://wails.io)):

```bash
./carbonio-files-client
```

- **Login**: enter the Carbonio **server**, **username**, and **password**. The UI language follows the OS locale (currently English and Italian ship out of the box, falling back to English).
- **Saved credentials**: on a successful login, the credentials are saved encrypted at rest in a per-user SQLite database (see [Configuration storage (SQLite)](#configuration-storage-sqlite)) — separate from `config.yaml` and from the CLI's `./file_sync_cache.db`. Location: `$XDG_CONFIG_HOME/carbonio-files-client/gui-config.db` on Linux (`~/.config/...` by default), `~/Library/Application Support/carbonio-files-client/gui-config.db` on macOS, `%AppData%\carbonio-files-client\gui-config.db` on Windows.
- **Auto-login**: on the next launch, if credentials were saved, the app logs in automatically. If that login fails, the login screen is shown again, pre-filled with the saved server/username, with a message explaining why:

  | Login outcome | Shown to the user |
  |---|---|
  | Wrong password, locked/inactive account, or account in maintenance mode (server returns HTTP 401 — the Carbonio auth endpoint does not distinguish these on the wire) | "Invalid username or password." |
  | Password expired (server redirects to the change-password page) | "Your password has expired. Change it from Carbonio webmail, then sign in again." |
  | Account/domain not authorized (HTTP 403) | "Access to this server was denied for this account." |
  | Server unreachable (DNS/connection/TLS/timeout) | "Could not reach the server..." |
  | Server error (HTTP 5xx) | "The server is currently unavailable..." |

- **Dashboard**: after login, a sidebar menu gives access to the Dashboard and, under **Preferences**, **Authentication** — showing the connected server/username with a **Log out** button that clears the in-memory session and removes the saved credentials.

### Building/running with the Wails CLI (optional)

For live-reload development, install the [Wails CLI](https://wails.io/docs/gettingstarted/installation) and run from `cmd/carbonio-files-go-client`:

```bash
wails dev -tags webkit2_41
```

## Configuration

Create a `config.yaml` file in the directory where you run the client:

```yaml
Main:
  endpoint: "mail.example.com"   # Carbonio server hostname or IP
  username: "myuser"             # Carbonio account username
  password: "mypassword"         # Carbonio account password
#  AuthToken: "ZM_AUTH_TOKEN"    # Optional: pre-computed auth token (skips login)
#  filesLocalFolder: "./files"   # Optional: by default it create the folder "files" where you are running carbonio-files-go-client
```

When `AuthToken` is provided, the username/password login step is skipped and the token is used directly.

## Usage

```
./carbonio-files-client -cli -[FLAG] [OPTIONS]
```

Every flag below requires the top-level `-cli` flag: it is what unlocks the command-line interface. Running the binary with any other flag but without `-cli` is rejected; running it with no arguments at all opens the GUI instead (see [Desktop GUI](#desktop-gui)).

Print all available flags:

```bash
./carbonio-files-client -cli -v
```

### List all remote nodes

Recursively list all files and folders in remote storage:

```bash
./carbonio-files-client -cli -getAllNode
```

### Download all files

Recursively download all remote files to the local `./files/` directory (created automatically):

```bash
./carbonio-files-client -cli -downloadAllFiles
```

A progress bar is shown for each file being downloaded.

### Upload a file

```bash
./carbonio-files-client -cli -uploadFile "/path/to/file.txt" -parentId "<parent-node-id>"
```

Use `-getAllNode` to find a parent node ID. The root of your personal files is typically `LOCAL_ROOT`.

### Upload a new file version

```bash
./carbonio-files-client -cli -uploadNewVersionFile "/path/to/file.txt" \
  -nodeId "<existing-node-id>" \
  -parentId "<parent-node-id>" \
  [-overwriteVersion]
```

Pass `-overwriteVersion` to overwrite the latest version instead of creating a new one.

### Create a remote folder

```bash
./carbonio-files-client -cli -createFolder "FolderName" -parentId "<parent-node-id>"
```

### Move nodes

Move one or more nodes to a different folder:

```bash
./carbonio-files-client -cli -moveNodes \
  -nodesIdList "id1,id2,id3" \
  -destinationId "<destination-folder-id>"
```

### Trash nodes (soft delete)

Move nodes to trash (recoverable):

```bash
./carbonio-files-client -cli -trashNodes -nodesIdList "id1,id2"
```

### Delete nodes (permanent)

Permanently delete nodes:

```bash
./carbonio-files-client -cli -deleteNodes -nodesIdList "id1,id2,id3"
```

### Check sync differences (no cache)

Compare the local `./files/` directory with the remote storage and report differences without making any changes:

```bash
./carbonio-files-client -cli -liveSyncCheck
```

Differences reported include: missing paths, digest mismatches, size differences, and timestamp differences.

### Update the sync cache

Populate the SQLite cache database (`./file_sync_cache.db`) with the current state of both local and remote files. Run this before any `-liveCacheSync`:

```bash
./carbonio-files-client -cli -updateCacheSync
```

### Bidirectional sync with cache

Perform a smart bidirectional sync using the cache:

```bash
./carbonio-files-client -cli -liveCacheSync
```

The sync proceeds in phases:

1. **Download** — fetches items that exist only on the remote to `./files/`.
2. **Upload** — uploads items that exist only locally to the remote.
3. **Clean local** — removes local items that were deleted on the remote.
4. **Trash remote** — trashes remote items that were deleted locally.
5. **Update out_of_sync** — find all out_of_sync items then compare remote and local, finally upload or download new version.

out_of_sync function use compare algorithm with **digest / file size / modify timestamp**.

The cache database is updated after each operation.

## Flags reference

| Flag | Type | Description |
|------|------|-------------|
| `-cli` | bool | Unlock the command-line interface (required before any other flag) |
| `-getAllNode` | bool | Recursively list all remote nodes |
| `-downloadAllFiles` | bool | Download all remote files to `./files/` |
| `-uploadFile` | string | Path to local file to upload |
| `-uploadNewVersionFile` | string | Path to local file to upload as a new version |
| `-createFolder` | string | Name of the remote folder to create |
| `-moveNodes` | bool | Move nodes specified by `-nodesIdList` |
| `-deleteNodes` | bool | Permanently delete nodes specified by `-nodesIdList` |
| `-trashNodes` | bool | Move nodes to trash |
| `-liveSyncCheck` | bool | Compare local and remote without making changes |
| `-updateCacheSync` | bool | Update the SQLite sync cache (run before any `-liveSyncCheck`)|
| `-liveCacheSync` | bool | Perform bidirectional sync using the cache |
| `-parentId` | string | Parent folder node ID (used with upload/create operations) |
| `-nodeId` | string | Node ID (used with `-uploadNewVersionFile`) |
| `-nodesIdList` | string | Comma-separated list of node IDs |
| `-destinationId` | string | Destination folder node ID (used with `-moveNodes`) |
| `-overwriteVersion` | bool | Overwrite latest version when uploading a new version |
| `-v` | bool | Print all available flags |

## Project structure

```
carbonio-files-go-client/
├── config/
│   └── config.yaml              # Configuration template
├── cmd/
│   └── carbonio-files-go-client/
│       ├── main.go              # Entry point: dispatches to GUI (default) or CLI (-cli)
│       ├── app.go               # Wails-bound GUI backend (login, session, auto-login)
│       ├── app_test.go          # GUI login/persist/auto-login/logout tests
│       ├── wails.json           # Wails project config
│       └── frontend/dist/       # Plain HTML/CSS/JS GUI (no JS build step)
├── pkg/
│   ├── carbonio/
│   │   └── carbonio.go          # HTTP auth (classified AuthError) and file transfer
│   ├── graphql/
│   │   ├── graphqlClient.go     # GraphQL client wrapper
│   │   ├── graphqlAPI.go        # High-level API operations
│   │   ├── getChildren.go       # GetChildren query
│   │   ├── createFolder.go      # CreateFolder mutation
│   │   ├── moveNodes.go         # MoveNodes mutation
│   │   ├── deleteNodes.go       # DeleteNodes mutation
│   │   └── trashNodes.go        # TrashNodes mutation
│   ├── i18n/
│   │   ├── i18n.go              # Locale resolution + catalog loading
│   │   ├── locale_linux.go      # OS locale detection (linux/darwin/windows)
│   │   └── locales/             # en.json, it.json translation catalogs
│   ├── localfs/
│   │   └── localfilesystem.go   # Local file system operations
│   └── sqlite/
│       ├── sqlitecache.go       # SQLite sync cache
│       └── sqliteconfig.go      # Encrypted-at-rest configuration table (CRUD)
├── files_watcher.go             # File system watcher utility
├── Makefile
├── go.mod
└── go.sum
```

## Dependencies

| Package | Purpose |
|---------|---------|
| [Khan/genqlient](https://github.com/Khan/genqlient) | GraphQL client code generation |
| [andybalholm/brotli](https://github.com/andybalholm/brotli) | Brotli decompression |
| [fsnotify/fsnotify](https://github.com/fsnotify/fsnotify) | File system event notifications |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | Unicode text normalization |
| [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) | YAML configuration parsing |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | Pure-Go SQLite driver |

## Configuration storage (SQLite)

In addition to `config.yaml`, the same `Main` configuration can be persisted in
the SQLite database managed by `pkg/sqlite` (`sqlitecache.SqliteHelper`), in a
singleton `config` table (`id` is always `1`). `Password` and `AuthToken` are
encrypted at rest with AES-256-GCM before being written; every other field
(`endpoint`, `username`, `files_local_folder`) is stored as plain text. This is
exactly the mechanism the [desktop GUI](#desktop-gui) uses to remember your
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

## Testing

```bash
make test
```

## Notes

- The Carbonio server TLS certificate is not verified (`InsecureSkipVerify`). This allows connections to servers using self-signed certificates.
- Downloads are performed with a maximum of one concurrent operation.
- File integrity is verified using SHA-384 digests.
- The SQLite cache file (`./file_sync_cache.db`) is created automatically in the current directory when using cache-based sync operations.
- The local download/sync directory (`./files/`) is created automatically when needed.
- The configuration table's AES-256 key (`<dbPath>.key`, e.g. `./file_sync_cache.db.key`) holds the material needed to decrypt stored credentials; treat it like a secret and never commit it (both `*.db` and `*.db.key` are gitignored).
- The desktop GUI's credential database (`gui-config.db` under the OS config directory) and its `.key` sibling are as sensitive as the CLI's `./file_sync_cache.db`/`.key` and are never written to the project directory.
