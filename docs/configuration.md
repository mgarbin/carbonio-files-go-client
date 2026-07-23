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

Logging:
#  level: "info"      # trace, debug, info, warn, error, fatal, panic, disabled (default: info)
#  format: "console"  # "console" (human-readable, colorized) or "json" (default: console)
#  output: "console"  # "console", "file" or "both" (default: console)
#  path: "logs/carbonio-files-go-client.log"  # log file path, used when output is "file" or "both"
```

When `AuthToken` is provided, the username/password login step is skipped and the token is used directly.

Every `Logging` field is optional and overridable from the command line (see
[Logging](logging.md) below); the `Logging` block itself may be omitted entirely
to use the built-in defaults (info level, console format, console output).
