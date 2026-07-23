[← Back to README](../README.md)

## Project structure

```
carbonio-files-go-client/
├── config/
│   └── config.yaml              # Configuration template
├── cmd/
│   └── carbonio-files-go-client/
│       ├── main.go              # Entry point: dispatches to GUI (default) or CLI (-cli)
│       ├── app.go               # Wails-bound GUI backend (login, credential test/save, sync-folder wizard, logging prefs)
│       ├── app_test.go          # GUI login/persist/auto-login/logout/sync-folder/credential-test tests
│       ├── wails.json           # Wails project config
│       └── frontend/             # Svelte + Tailwind GUI (Vite build); src/ is source, dist/ is the embedded build output
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
