# carbonio-files-go-client

A command-line client for managing and synchronizing files with a [Carbonio](https://www.zextras.com/carbonio/) server. It supports uploading, downloading, listing, moving, trashing, deleting nodes, and performing bidirectional file synchronization backed by a local SQLite cache.

## License

This project is licensed under the [GNU Affero General Public License v3](COPYING).

## Requirements

- Go 1.24 or later
- Access to a Carbonio server

## Installation

### Build from source

```bash
make build
```

This produces the `carbonio-files-client` binary in the project root (symbols stripped and optimized).

## Configuration

Create a `config.yaml` file in the directory where you run the client:

```yaml
Main:
  server: "mail.example.com"    # Carbonio server hostname or IP
  username: "myuser"            # Carbonio account username
  password: "mypassword"        # Carbonio account password
#  AuthToken: "ZM_AUTH_TOKEN"  # Optional: pre-computed auth token (skips login)
```

When `AuthToken` is provided, the username/password login step is skipped and the token is used directly.

## Usage

```
./carbonio-files-client -[FLAG] [OPTIONS]
```

Print all available flags:

```bash
./carbonio-files-client -v
```

### List all remote nodes

Recursively list all files and folders in remote storage:

```bash
./carbonio-files-client -getAllNode
```

### Download all files

Recursively download all remote files to the local `./files/` directory (created automatically):

```bash
./carbonio-files-client -downloadAllFiles
```

A progress bar is shown for each file being downloaded.

### Upload a file

```bash
./carbonio-files-client -uploadFile "/path/to/file.txt" -parentId "<parent-node-id>"
```

Use `-getAllNode` to find a parent node ID. The root of your personal files is typically `LOCAL_ROOT`.

### Upload a new file version

```bash
./carbonio-files-client -uploadNewVersionFile "/path/to/file.txt" \
  -nodeId "<existing-node-id>" \
  -parentId "<parent-node-id>" \
  [-overwriteVersion]
```

Pass `-overwriteVersion` to overwrite the latest version instead of creating a new one.

### Create a remote folder

```bash
./carbonio-files-client -createFolder "FolderName" -parentId "<parent-node-id>"
```

### Move nodes

Move one or more nodes to a different folder:

```bash
./carbonio-files-client -moveNodes \
  -nodesIdList "id1,id2,id3" \
  -destinationId "<destination-folder-id>"
```

### Trash nodes (soft delete)

Move nodes to trash (recoverable):

```bash
./carbonio-files-client -trashNodes -nodesIdList "id1,id2"
```

### Delete nodes (permanent)

Permanently delete nodes:

```bash
./carbonio-files-client -deleteNodes -nodesIdList "id1,id2,id3"
```

### Check sync differences (no cache)

Compare the local `./files/` directory with the remote storage and report differences without making any changes:

```bash
./carbonio-files-client -liveSyncCheck
```

Differences reported include: missing paths, digest mismatches, size differences, and timestamp differences.

### Initialize the sync cache

Populate the SQLite cache database (`./file_sync_cache.db`) with the current state of both local and remote files. Run this before the first `-liveCacheSync`:

```bash
./carbonio-files-client -initCacheSync
```

### Bidirectional sync with cache

Perform a smart bidirectional sync using the cache:

```bash
./carbonio-files-client -liveCacheSync
```

The sync proceeds in four phases:

1. **Download** — fetches items that exist only on the remote to `./files/`.
2. **Upload** — uploads items that exist only locally to the remote.
3. **Clean local** — removes local items that were deleted on the remote.
4. **Trash remote** — trashes remote items that were deleted locally.

The cache database is updated after each operation.

## Flags reference

| Flag | Type | Description |
|------|------|-------------|
| `-getAllNode` | bool | Recursively list all remote nodes |
| `-downloadAllFiles` | bool | Download all remote files to `./files/` |
| `-uploadFile` | string | Path to local file to upload |
| `-uploadNewVersionFile` | string | Path to local file to upload as a new version |
| `-createFolder` | string | Name of the remote folder to create |
| `-moveNodes` | bool | Move nodes specified by `-nodesIdList` |
| `-deleteNodes` | bool | Permanently delete nodes specified by `-nodesIdList` |
| `-trashNodes` | bool | Move nodes to trash |
| `-liveSyncCheck` | bool | Compare local and remote without making changes |
| `-initCacheSync` | bool | Initialize the SQLite sync cache |
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
│       └── main.go              # CLI entry point
├── pkg/
│   ├── carbonio/
│   │   └── carbonio.go          # HTTP auth and file transfer
│   ├── graphql/
│   │   ├── graphqlClient.go     # GraphQL client wrapper
│   │   ├── graphqlAPI.go        # High-level API operations
│   │   ├── getChildren.go       # GetChildren query
│   │   ├── createFolder.go      # CreateFolder mutation
│   │   ├── moveNodes.go         # MoveNodes mutation
│   │   ├── deleteNodes.go       # DeleteNodes mutation
│   │   └── trashNodes.go        # TrashNodes mutation
│   ├── localfs/
│   │   └── localfilesystem.go   # Local file system operations
│   └── sqlite/
│       └── sqlitecache.go       # SQLite sync cache
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
