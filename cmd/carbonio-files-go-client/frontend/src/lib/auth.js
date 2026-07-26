import { session, view, section, loginBusy, loginError, loginPrefill, resetSessionState } from "./stores";
import * as api from "./api";

// handleLoginResult applies the outcome of any login attempt - a manual
// form submit or Init()'s auto-login - to the shared session/view state.
export function handleLoginResult(result) {
  loginBusy.set(false);
  if (result && result.success) {
    session.set({ endpoint: result.endpoint, username: result.username });
    loginError.set(null);
    if (result.needsSyncSetup) {
      view.set("sync-setup");
    } else {
      view.set("dashboard");
      section.set("dashboard");
    }
  } else {
    loginError.set((result && result.errorKind) || "unknown");
    loginPrefill.update((p) => ({
      endpoint: (result && result.endpoint) || p.endpoint,
      username: (result && result.username) || p.username,
    }));
  }
}

export function submitLogin(endpoint, username, password) {
  loginBusy.set(true);
  loginError.set(null);
  return api
    .login(endpoint, username, password)
    .then(handleLoginResult)
    .catch((err) => {
      loginBusy.set(false);
      loginError.set("unknown");
      loginPrefill.set({ endpoint, username });
      console.error(err);
    });
}

export function logout() {
  return api.logout().finally(() => {
    session.set(null);
    resetSessionState();
  });
}
