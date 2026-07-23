[← Back to README](../README.md)

## Usage

```
./carbonio-files-client -cli -[FLAG] [OPTIONS]
```

Every flag below requires the top-level `-cli` flag: it is what unlocks the command-line interface. Running the binary with any other flag but without `-cli` is rejected; running it with no arguments at all opens the GUI instead (see [Desktop GUI](desktop-gui.md)).

**Authentication**: unless `config.yaml` sets an explicit `AuthToken` override, every `-cli` run reuses the `ZM_AUTH_TOKEN` cached (encrypted at rest) from the previous run in `<home>/.carbonio_files_sync/file_sync_cache.db` — no login round-trip — for as long as the server still accepts it, and transparently re-authenticates with `username`/`password` (persisting the refreshed token) the moment it doesn't. See [Configuration storage (SQLite)](configuration-storage-sqlite.md).

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

Populate the SQLite cache database (`<home>/.carbonio_files_sync/file_sync_cache.db`) with the current state of both local and remote files. Run this before any `-liveCacheSync`:

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

### Update cache and sync in one step

Run `-updateCacheSync` followed by `-liveCacheSync` in a single command, useful for unattended/scheduled runs:

```bash
./carbonio-files-client -cli -fullCacheSync
```
