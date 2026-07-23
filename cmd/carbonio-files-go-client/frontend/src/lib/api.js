// Thin wrapper around the Wails-bound Go backend (window.go.main.App.*,
// injected by the Wails runtime - see ../../app.go). Centralizing the
// calls here keeps every other module free of the `window.go` global.
function backend() {
  if (!window.go || !window.go.main || !window.go.main.App) {
    throw new Error("Wails backend binding (window.go.main.App) is not available");
  }
  return window.go.main.App;
}

export const init = () => backend().Init();
export const login = (endpoint, username, password) => backend().Login(endpoint, username, password);
export const testLogin = (endpoint, username, password) => backend().TestLogin(endpoint, username, password);
export const logout = () => backend().Logout();
export const chooseSyncFolder = () => backend().ChooseSyncFolder();
export const setSyncFolder = (path) => backend().SetSyncFolder(path);
export const getSyncFolder = () => backend().GetSyncFolder();
export const chooseLogFolder = (currentPath) => backend().ChooseLogFolder(currentPath);
export const getLoggingConfig = () => backend().GetLoggingConfig();
export const updateLoggingConfig = (level, format, output, path) =>
  backend().UpdateLoggingConfig(level, format, output, path);
export const openLogFile = (path) => backend().OpenLogFile(path);
