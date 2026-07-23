// Applies the persisted theme before the first paint.
import "./lib/theme";
import "./app.css";
import App from "./App.svelte";

const app = new App({
  target: document.getElementById("app"),
});

export default app;
