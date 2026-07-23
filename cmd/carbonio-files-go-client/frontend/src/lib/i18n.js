import { get } from "svelte/store";
import { translations } from "./stores";

// t looks up `key` in the catalog delivered by the Go backend's Init()
// call, falling back to the raw key (matches the original app.js
// behavior, and makes missing translations obvious instead of blank).
export function t(key) {
  const dict = get(translations);
  return (dict && dict[key]) || key;
}

// errorMessage maps a backend ErrorKind (e.g. "invalid_credentials") to a
// localized message, falling back to the generic "unknown" message when
// the kind has no dedicated translation.
export function errorMessage(kind) {
  if (!kind) return null;
  const key = "error." + kind;
  const msg = t(key);
  return msg !== key ? msg : t("error.unknown");
}
