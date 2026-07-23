import { writable, get } from "svelte/store";

const STORAGE_KEY = "cf-theme";

// Named themes this build ships. Each one is just a CSS class that
// redefines the custom properties in src/app.css - adding a new theme
// later (e.g. "contrast") means adding a class block there and a value
// here, nothing else. "system" is a meta-preference, not a real theme: it
// resolves to "light" or "dark" from the OS setting at apply-time.
export const THEMES = ["light", "dark"];

function readStoredPreference() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "system" || THEMES.includes(saved)) return saved;
  } catch (err) {
    // localStorage unavailable (e.g. private browsing) - fall back silently.
  }
  return "system";
}

function systemPrefersDark() {
  return typeof window !== "undefined" && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function resolve(preference) {
  return preference === "system" ? (systemPrefersDark() ? "dark" : "light") : preference;
}

function applyToDocument(resolved) {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", resolved === "dark");
  document.documentElement.style.colorScheme = resolved;
}

// themePreference is what the user picked ("light" | "dark" | "system").
// resolvedTheme is the theme actually applied - identical to
// themePreference except when the preference is "system", in which case it
// tracks the live OS setting.
export const themePreference = writable(readStoredPreference());
export const resolvedTheme = writable(resolve(get(themePreference)));

themePreference.subscribe((preference) => {
  try {
    localStorage.setItem(STORAGE_KEY, preference);
  } catch (err) {
    // Ignore persistence failures - the theme still applies for this session.
  }
  const resolved = resolve(preference);
  resolvedTheme.set(resolved);
  applyToDocument(resolved);
});

export function setTheme(preference) {
  themePreference.set(preference);
}

// Keeps "system" live: re-resolves whenever the OS color scheme changes
// while the user's preference is "system".
if (typeof window !== "undefined" && window.matchMedia) {
  const mql = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => {
    if (get(themePreference) === "system") {
      const resolved = resolve("system");
      resolvedTheme.set(resolved);
      applyToDocument(resolved);
    }
  };
  if (mql.addEventListener) mql.addEventListener("change", onChange);
  else mql.addListener(onChange);
}
