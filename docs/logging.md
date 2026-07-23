[← Back to README](../README.md)

## Logging

Logging is powered by [rs/zerolog](https://github.com/rs/zerolog). Every log
line goes through a single process-wide logger (`pkg/logger`) that can be
configured independently along two axes:

- **Format** - `console` (human-readable, colorized) or `json` (one JSON object per line, suited for log aggregators).
- **Output** - `console` (stderr), `file`, or `both`.

Precedence for the CLI (`-cli`) is `-logLevel`/`-logFormat`/`-logOutput`/`-logPath`
flags > the `Logging` section of `config.yaml` > built-in defaults
(`info` / `console` / `console` / `<home>/.carbonio_files_sync/carbonio-files-go-client.log`).
The log file's parent directory is created automatically if it doesn't exist.

```bash
# JSON logs written to a file, nothing on the console
./carbonio-files-client -cli -getAllNode -logFormat json -logOutput file -logPath /var/log/carbonio-files-client.log

# Debug-level logs on both console and file
./carbonio-files-client -cli -liveCacheSync -logLevel debug -logOutput both
```

The [desktop GUI](desktop-gui.md) has no CLI flags; instead its logging settings
are persisted in the same encrypted SQLite config row as the saved
credentials (see [Configuration storage (SQLite)](configuration-storage-sqlite.md)),
read on startup, editable from **Preferences > Logging**, and backed by the
`App.GetLoggingConfig` / `App.UpdateLoggingConfig` Wails-bound methods. An
**Open log file** button next to **Save** opens the file currently shown in
the path field with the OS' default program for it (`App.OpenLogFile`,
e.g. `xdg-open` on Linux, `open` on macOS, the shell file association on
Windows) - it errors if that file doesn't exist yet, e.g. Output is still
"console" or nothing has been logged since the path last changed.
