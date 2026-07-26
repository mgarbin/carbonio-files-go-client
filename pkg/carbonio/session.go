package carbonio

import (
	"sync"

	sqlitecache "carbonio-files-go-client/pkg/sqlite"

	"github.com/rs/zerolog/log"
)

// Session manages the ZM_AUTH_TOKEN lifecycle for one set of credentials.
// It is the single place that implements "store the token encrypted at
// rest, reuse it across runs as long as the server still accepts it, and
// transparently re-authenticate with username/password the moment it
// doesn't" - shared verbatim by the CLI (cmd/carbonio-files-go-client's
// runCLI) and the desktop GUI (App.autoLogin/App.login), so the policy is
// implemented exactly once.
//
// A nil Store disables persistence entirely: Login/Reauthenticate then
// always perform a full username/password login and never touch disk,
// which keeps callers like the GUI's TestLogin (verify credentials without
// saving anything) trivially correct by construction.
type Session struct {
	Auth     *HTTPAuthenticator
	Store    *sqlitecache.SqliteHelper
	Username string
	Password string

	mu    sync.Mutex
	token string
}

// NewSession constructs a Session ready to call Login. auth.Endpoint must
// already be set.
func NewSession(auth *HTTPAuthenticator, store *sqlitecache.SqliteHelper, username, password string) *Session {
	return &Session{Auth: auth, Store: store, Username: username, Password: password}
}

// Login returns a usable ZM_AUTH_TOKEN. If Store holds a token saved from a
// previous run, it is validated against the server (ValidateToken) and
// reused as-is when still accepted - no password ever leaves the client in
// that case. Otherwise (no stored token, a stored token the server now
// rejects, or Store is nil) Login performs a full username/password login
// and, when Store is set, persists the fresh token so the next Login call
// can reuse it again.
func (s *Session) Login() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Store != nil {
		cfg, err := s.Store.GetConfig()
		if err != nil {
			log.Warn().Err(err).Str("username", s.Username).Msg("[auth] cannot read cached auth token, forcing re-login")
		} else if cfg != nil && cfg.AuthToken != "" {
			log.Debug().Str("username", s.Username).Msg("[auth] found cached auth token, validating against server")
			status, err := s.Auth.ValidateToken(cfg.AuthToken)
			switch {
			case err != nil:
				logWithAuthError(log.Warn(), err).Str("username", s.Username).
					Msg("[auth] cannot validate cached auth token against server, forcing re-login")
			case status == TokenValid:
				log.Debug().Str("username", s.Username).Msg("[auth] reusing cached auth token")
				s.token = cfg.AuthToken
				return s.token, nil
			default:
				log.Info().Str("username", s.Username).Str("tokenStatus", string(status)).
					Msg("[auth] cached auth token no longer accepted by server, re-authenticating")
			}
		} else {
			log.Debug().Str("username", s.Username).Msg("[auth] no cached auth token found, performing password login")
		}
	}

	return s.reauthenticate()
}

// Reauthenticate discards any cached token and performs a fresh
// username/password login, persisting the new token when Store is set.
// Callers that observe a 401 mid-session should call this and retry the
// failed request with the new token instead of calling Login again (Login
// would otherwise just re-validate the same now-rejected token).
func (s *Session) Reauthenticate() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reauthenticate()
}

// reauthenticate performs the password login and persistence; callers must
// hold s.mu.
func (s *Session) reauthenticate() (string, error) {
	log.Debug().Str("username", s.Username).Msg("[auth] no usable cached auth token, performing username/password login")
	token, err := s.Auth.CarbonioZxAuth(s.Username, s.Password)
	if err != nil {
		logWithAuthError(log.Error(), err).Str("username", s.Username).
			Msg("[auth] username/password login failed, cannot obtain a fresh auth token")
		return "", err
	}
	log.Info().Str("username", s.Username).Msg("[auth] username/password login succeeded, obtained a fresh auth token")
	s.token = *token

	if s.Store != nil {
		record := sqlitecache.ConfigRecord{
			Endpoint:  s.Auth.Endpoint,
			Username:  s.Username,
			Password:  s.Password,
			AuthToken: s.token,
		}
		// Preserve any previously saved logging settings, sync folder, sync
		// on/off decision, sync interval, and remote delete mode: UpsertConfig
		// replaces the whole singleton row, and refreshing the token should
		// never silently reset them to defaults (e.g. the dashboard's
		// Preferences > Synchronization "Modalità di eliminazione degli
		// oggetti remoti" dropdown reverting to "trash" every time the app is
		// relaunched and re-authenticates because no valid cached token
		// survived).
		if existing, err := s.Store.GetConfig(); err == nil && existing != nil {
			record.LogLevel = existing.LogLevel
			record.LogFormat = existing.LogFormat
			record.LogOutput = existing.LogOutput
			record.LogPath = existing.LogPath
			record.FilesLocalFolder = existing.FilesLocalFolder
			record.SyncEnabled = existing.SyncEnabled
			record.SyncIntervalMinutes = existing.SyncIntervalMinutes
			record.DeleteRemoteNode = existing.DeleteRemoteNode
		}
		if err := s.Store.UpsertConfig(record); err != nil {
			log.Error().Err(err).Str("username", s.Username).Msg("[auth] cannot persist refreshed auth token")
		} else {
			log.Debug().Str("username", s.Username).Msg("[auth] refreshed auth token cached for reuse on next run")
		}
	}

	return s.token, nil
}

// Token returns the token obtained by the last successful Login/
// Reauthenticate call, or "" if neither has been called yet.
func (s *Session) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}
