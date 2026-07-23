[← Back to README](../README.md)

## Desktop GUI

Running the binary with **no arguments** opens the desktop GUI (built with [Wails](https://wails.io)):

```bash
./carbonio-files-client
```

- **Login**: enter the Carbonio **server**, **username**, and **password**. The UI language follows the OS locale (currently English and Italian ship out of the box, falling back to English).
- **Saved credentials**: on a successful login, the credentials are saved encrypted at rest in a per-user SQLite database (see [Configuration storage (SQLite)](configuration-storage-sqlite.md)) — separate from `config.yaml` and from the CLI's `./file_sync_cache.db`. Location: `$XDG_CONFIG_HOME/carbonio-files-client/gui-config.db` on Linux (`~/.config/...` by default), `~/Library/Application Support/carbonio-files-client/gui-config.db` on macOS, `%AppData%\carbonio-files-client\gui-config.db` on Windows.
- **Auto-login**: on the next launch, if credentials were saved, the app logs in automatically. If that login fails, the login screen is shown again, pre-filled with the saved server/username, with a message explaining why:

  | Login outcome | Shown to the user |
  |---|---|
  | Wrong password, locked/inactive account, or account in maintenance mode (server returns HTTP 401 — the Carbonio auth endpoint does not distinguish these on the wire) | "Invalid username or password." |
  | Password expired (server redirects to the change-password page) | "Your password has expired. Change it from Carbonio webmail, then sign in again." |
  | Account/domain not authorized (HTTP 403) | "Access to this server was denied for this account." |
  | Server unreachable (DNS/connection/TLS/timeout) | "Could not reach the server..." |
  | Server error (HTTP 5xx) | "The server is currently unavailable..." |

- **First-login setup wizard**: right after the first successful login (when no sync folder has been configured yet), the app shows a wizard screen prompting you to pick the local folder to sync files into, using the **native OS folder picker** (Explorer on Windows, Finder on macOS, the desktop's file chooser on Linux). The choice is saved to the same SQLite config row as the credentials and the folder is created if missing; you're then taken to the dashboard. Subsequent logins skip the wizard.
- **Dashboard**: after login (and, on first login, after the setup wizard), a sidebar menu gives access to the Dashboard and, under **Preferences**:
  - **Authentication** — edit the **server**, **username**, and **password** and click **Test connection** to verify them against the server without saving anything. Test connection is enabled only once you've changed at least one field; Save is enabled only after a test succeeds for the exact values currently in the form (any further edit re-locks Save until you test again). A **Log out** button clears the in-memory session and removes the saved credentials.
  - **Sync folder** — shows the currently configured local sync folder and a **Change folder…** button that reopens the native OS folder picker and persists the new choice.
  - **Logging** — lets you change the log **level** (trace…disabled), **format** (console/JSON), and **output** (console/file/both, with a file path field), applied immediately and persisted for future launches.

### Desktop GUI frontend

The GUI is a [Svelte](https://svelte.dev) single-page app styled with [Tailwind CSS](https://tailwindcss.com), built with [Vite](https://vitejs.dev), and embedded into the Go binary via `go:embed` (see `cmd/carbonio-files-go-client/main.go`). Source lives in `cmd/carbonio-files-go-client/frontend/src/`; `make build`/`make test` run `npm install`/`npm run build` automatically and the compiled output lands in `frontend/dist/`, which is what gets embedded (also committed to git, so a checkout builds even without running the frontend step first).

- Every backend call goes through `src/lib/api.js`, a thin wrapper over the Wails-injected `window.go.main.App.*` bindings.
- App-wide state (session, current view/section, cached preferences panels) lives in Svelte stores under `src/lib/stores.js`; `src/lib/auth.js` holds the login/logout flow shared between manual sign-in and Init()'s auto-login.
- **Theming**: every color in the UI resolves through a CSS custom property (`--color-*`, defined in `src/app.css`) instead of a hardcoded value, and Tailwind's semantic color tokens (`bg-surface`, `text-muted`, `bg-brand`, ...) read those properties (see `tailwind.config.js`). Switching themes is just toggling a `.dark` class on `<html>`; adding a new theme is a new CSS class block plus an entry in `src/lib/theme.js`'s `THEMES` list, no component changes needed. The sidebar's **Theme** control lets the user pick Light, Dark, or System (follows the OS setting live), persisted to `localStorage`.

### Building/running with the Wails CLI (optional)

For live-reload development, install the [Wails CLI](https://wails.io/docs/gettingstarted/installation) and run from `cmd/carbonio-files-go-client` (it drives `npm install`/`npm run dev` in `frontend/` for you, per `wails.json`'s `frontend:*` hooks):

```bash
wails dev -tags webkit2_41
```

To iterate on the frontend alone (in a regular browser, backend calls mocked or unavailable), run `npm run dev` directly from `cmd/carbonio-files-go-client/frontend`.
