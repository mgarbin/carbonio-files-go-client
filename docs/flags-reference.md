[← Back to README](../README.md)

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
| `-fullCacheSync` | bool | Run `-updateCacheSync` followed by `-liveCacheSync` in one step |
| `-parentId` | string | Parent folder node ID (used with upload/create operations) |
| `-nodeId` | string | Node ID (used with `-uploadNewVersionFile`) |
| `-nodesIdList` | string | Comma-separated list of node IDs |
| `-destinationId` | string | Destination folder node ID (used with `-moveNodes`) |
| `-overwriteVersion` | bool | Overwrite latest version when uploading a new version |
| `-v` | bool | Print all available flags |
| `-logLevel` | string | Log level: trace, debug, info, warn, error, fatal, panic, disabled (default: info, or `config.yaml` `Logging.level`) |
| `-logFormat` | string | Log format: console or json (default: console, or `config.yaml` `Logging.format`) |
| `-logOutput` | string | Log output: console, file or both (default: console, or `config.yaml` `Logging.output`) |
| `-logPath` | string | Log file path, used when `-logOutput` is file or both (default: `<home>/.carbonio_files_sync/carbonio-files-go-client.log`, or `config.yaml` `Logging.path`) |
