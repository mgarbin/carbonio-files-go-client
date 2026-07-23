<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { logging } from "../lib/stores";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import Banner from "./ui/Banner.svelte";
  import PanelCard from "./ui/PanelCard.svelte";
  import SelectInput from "./ui/SelectInput.svelte";
  import Button from "./ui/Button.svelte";

  const LOG_LEVELS = ["trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"];
  const LOG_FORMATS = ["console", "json"];
  const LOG_OUTPUTS = ["console", "file", "both"];

  // Seed the form from whatever's already cached (revisiting the panel
  // after a first load); loadIfNeeded fills these in once the fetch
  // resolves when nothing was cached yet.
  const cached = get(logging);
  let level = cached.level;
  let format = cached.format;
  let output = cached.output;
  let path = cached.path;
  let browsing = false;
  let opening = false;
  let openError = null;

  onMount(loadIfNeeded);

  function loadIfNeeded() {
    const lg = get(logging);
    if (lg.loaded || lg.loading) return;
    logging.update((s) => ({ ...s, loading: true }));
    api
      .getLoggingConfig()
      .then((cfg) => {
        const next = {
          loaded: true,
          loading: false,
          level: (cfg && cfg.level) || "info",
          format: (cfg && cfg.format) || "console",
          output: (cfg && cfg.output) || "console",
          path: (cfg && cfg.path) || "",
          busy: false,
          error: null,
          saved: false,
        };
        logging.set(next);
        level = next.level;
        format = next.format;
        output = next.output;
        path = next.path;
      })
      .catch((err) => {
        logging.update((s) => ({ ...s, loading: false, error: "generic" }));
        console.error(err);
      });
  }

  // browseLogFolder opens the native OS directory picker for the log
  // file's directory and writes the resulting path straight into `path`.
  // It intentionally doesn't touch $logging.busy: a full disable of the
  // form would be surprising while only the folder picker is open.
  function browseLogFolder() {
    browsing = true;
    api
      .chooseLogFolder(path)
      .then((fullPath) => {
        browsing = false;
        if (fullPath) path = fullPath;
      })
      .catch((err) => {
        browsing = false;
        console.error(err);
      });
  }

  // openLogFile asks the backend to open the currently shown log file
  // path with the OS' default program (e.g. Notepad, TextEdit, whichever
  // editor is associated with .log files). Uses `path` as typed, even if
  // not saved yet, so "open" always reflects what's on screen.
  function openLogFile() {
    opening = true;
    openError = null;
    api
      .openLogFile(path)
      .then(() => {
        opening = false;
      })
      .catch((err) => {
        opening = false;
        openError = "generic";
        console.error(err);
      });
  }

  function save() {
    logging.update((s) => ({ ...s, busy: true, error: null, saved: false }));
    api
      .updateLoggingConfig(level, format, output, path)
      .then(() => {
        logging.set({ loaded: true, loading: false, level, format, output, path, busy: false, error: null, saved: true });
      })
      .catch((err) => {
        logging.update((s) => ({ ...s, busy: false, error: "generic" }));
        console.error(err);
      });
  }
</script>

<h2 class="mb-5 mt-0 text-xl font-semibold">{t("logging.panelTitle")}</h2>

{#if $logging.loading && !$logging.loaded}
  <Banner kind="status">{t("common.loading")}</Banner>
{:else}
  <PanelCard>
    <form on:submit|preventDefault={save}>
      <SelectInput
        id="lg-level"
        label={t("logging.levelLabel")}
        bind:value={level}
        disabled={$logging.busy}
        options={LOG_LEVELS.map((l) => ({ value: l, label: t("logging.level." + l) }))}
      />
      <SelectInput
        id="lg-format"
        label={t("logging.formatLabel")}
        bind:value={format}
        disabled={$logging.busy}
        options={LOG_FORMATS.map((f) => ({ value: f, label: t("logging.format." + f) }))}
      />
      <SelectInput
        id="lg-output"
        label={t("logging.outputLabel")}
        bind:value={output}
        disabled={$logging.busy}
        options={LOG_OUTPUTS.map((o) => ({ value: o, label: t("logging.output." + o) }))}
      />

      <div class="mb-4">
        <label for="lg-path" class="mb-1.5 block text-[13px] font-semibold text-muted">{t("logging.pathLabel")}</label>
        <div class="flex items-center gap-2.5">
          <input
            id="lg-path"
            type="text"
            placeholder={t("logging.pathPlaceholder")}
            bind:value={path}
            disabled={$logging.busy || browsing}
            class="flex-1 rounded border border-border bg-surface px-3 py-2.5 text-sm text-text focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/[0.15] disabled:opacity-60"
          />
          <Button variant="secondary" disabled={$logging.busy || browsing} on:click={browseLogFolder}>
            {t("logging.browseButton")}
          </Button>
        </div>
      </div>

      {#if $logging.error}
        <Banner kind="error">{errorMessage($logging.error)}</Banner>
      {/if}
      {#if openError}
        <Banner kind="error">{errorMessage(openError)}</Banner>
      {/if}
      {#if $logging.saved}
        <Banner kind="success">{t("logging.savedNote")}</Banner>
      {/if}

      <div class="flex gap-2.5">
        <Button type="submit" full={false} disabled={$logging.busy}>{$logging.busy ? t("logging.saving") : t("logging.saveButton")}</Button>
        <Button type="button" variant="secondary" full={false} disabled={$logging.busy || opening} on:click={openLogFile}>
          {opening ? t("logging.opening") : t("logging.openButton")}
        </Button>
      </div>
    </form>
  </PanelCard>
{/if}
