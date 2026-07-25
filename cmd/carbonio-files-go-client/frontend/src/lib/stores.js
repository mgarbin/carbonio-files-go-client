import { writable } from "svelte/store";

// ---------- App shell ----------
export const booting = writable(true);
export const translations = writable({});
export const view = writable("login"); // 'login' | 'sync-setup' | 'dashboard'
export const section = writable("dashboard"); // 'dashboard' | 'docsOnline' | 'authentication' | 'syncFolder' | 'syncInterval' | 'logging'
export const session = writable(null); // { endpoint, username } | null

// ---------- Login screen ----------
export const loginBusy = writable(false);
export const loginError = writable(null); // errorKind string | null
export const loginPrefill = writable({ endpoint: "", username: "" });

// ---------- Preferences: sync folder ----------
// Cached across navigation (like the original single global `state`) so
// re-entering the panel doesn't re-fetch or lose the "saved" confirmation.
export function freshSyncFolder() {
  return { loaded: false, loading: false, path: "", busy: false, error: null, saved: false };
}
export const syncFolder = writable(freshSyncFolder());

// ---------- Preferences: sync interval + remote delete mode ----------
// How often (in minutes) the background sync job re-checks the cache and
// reconciles changes - one of 5, 15, 30 or 60 (see App.SetSyncIntervalMinutes).
// deleteRemoteNode is "trash" or "delete" (see App.SetDeleteRemoteNode) and
// controls how a locally-deleted item is propagated to the remote node.
export function freshSyncInterval() {
  return { loaded: false, loading: false, minutes: 5, deleteRemoteNode: "trash", busy: false, error: null, saved: false };
}
export const syncInterval = writable(freshSyncInterval());

// ---------- Preferences: logging ----------
export function freshLogging() {
  return {
    loaded: false,
    loading: false,
    level: "",
    format: "",
    output: "",
    path: "",
    busy: false,
    error: null,
    saved: false,
  };
}
export const logging = writable(freshLogging());

// resetSessionState restores every per-session store to its pristine state.
// Called on logout so the next login starts clean.
export function resetSessionState() {
  view.set("login");
  section.set("dashboard");
  loginPrefill.set({ endpoint: "", username: "" });
  loginError.set(null);
  syncFolder.set(freshSyncFolder());
  syncInterval.set(freshSyncInterval());
  logging.set(freshLogging());
}
