<script>
  import * as api from "../lib/api";
  import { t, errorMessage } from "../lib/i18n";
  import { view, section, syncFolder } from "../lib/stores";
  import Banner from "./ui/Banner.svelte";
  import Button from "./ui/Button.svelte";

  let path = "";
  let busy = false;
  let error = null;

  $: hasPath = !!path;

  function pickFolder() {
    api
      .chooseSyncFolder()
      .then((chosen) => {
        if (chosen) {
          path = chosen;
          error = null;
        }
      })
      .catch((err) => {
        error = "generic";
        console.error(err);
      });
  }

  function complete() {
    if (!path) return;
    busy = true;
    error = null;
    api
      .setSyncFolder(path)
      .then(() => {
        busy = false;
        // Prime the preferences panel so it doesn't need a reload.
        syncFolder.set({ loaded: true, loading: false, path, busy: false, error: null, saved: false });
        section.set("dashboard");
        view.set("dashboard");
      })
      .catch((err) => {
        busy = false;
        error = "generic";
        console.error(err);
      });
  }
</script>

<div class="flex h-full items-center justify-center p-6">
  <div class="w-full max-w-sm rounded border border-border bg-surface p-8 shadow-lg shadow-slate-950/10">
    <h1 class="mb-6 text-center text-xl font-bold">{t("syncSetup.heading")}</h1>
    <p class="text-center text-xs leading-relaxed text-muted">{t("syncSetup.body")}</p>

    {#if error}
      <Banner kind="error">{errorMessage(error)}</Banner>
    {/if}

    <div class="my-5 flex items-center gap-2.5">
      <div
        class="flex-1 rounded border border-border bg-bg px-3 py-2.5 text-[13px] break-all {hasPath
          ? 'text-text'
          : 'italic text-muted'}"
      >
        {path || t("syncSetup.noneChosen")}
      </div>
      <Button variant="secondary" disabled={busy} on:click={pickFolder}>{t("syncSetup.browseButton")}</Button>
    </div>

    <Button disabled={busy || !hasPath} on:click={complete}>
      {busy ? t("syncSetup.saving") : t("syncSetup.continueButton")}
    </Button>
  </div>
</div>
