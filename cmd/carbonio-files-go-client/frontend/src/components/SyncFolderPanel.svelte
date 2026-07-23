<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { syncFolder } from "../lib/stores";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import Banner from "./ui/Banner.svelte";
  import PanelCard from "./ui/PanelCard.svelte";
  import Button from "./ui/Button.svelte";

  onMount(loadIfNeeded);

  function loadIfNeeded() {
    const sf = get(syncFolder);
    if (sf.loaded || sf.loading) return;
    syncFolder.update((s) => ({ ...s, loading: true }));
    api
      .getSyncFolder()
      .then((settings) => {
        syncFolder.set({
          loaded: true,
          loading: false,
          path: (settings && settings.path) || "",
          busy: false,
          error: null,
          saved: false,
        });
      })
      .catch((err) => {
        syncFolder.update((s) => ({ ...s, loading: false, error: "generic" }));
        console.error(err);
      });
  }

  function pickAndSave() {
    syncFolder.update((s) => ({ ...s, busy: true, error: null, saved: false }));
    api
      .chooseSyncFolder()
      .then((path) => {
        if (!path) {
          // User cancelled the dialog.
          syncFolder.update((s) => ({ ...s, busy: false }));
          return;
        }
        return api.setSyncFolder(path).then(() => {
          syncFolder.update((s) => ({ ...s, busy: false, path, saved: true }));
        });
      })
      .catch((err) => {
        syncFolder.update((s) => ({ ...s, busy: false, error: "generic" }));
        console.error(err);
      });
  }
</script>

<h2 class="mb-5 mt-0 text-xl font-semibold">{t("syncFolder.panelTitle")}</h2>

{#if $syncFolder.loading && !$syncFolder.loaded}
  <Banner kind="status">{t("common.loading")}</Banner>
{:else}
  <PanelCard>
    {#if $syncFolder.error}
      <Banner kind="error">{errorMessage($syncFolder.error)}</Banner>
    {/if}

    <div class="flex justify-between py-2.5 text-sm">
      <span class="text-muted">{t("syncFolder.currentPath")}</span>
      <span class="font-semibold">{$syncFolder.path || t("syncSetup.noneChosen")}</span>
    </div>

    {#if $syncFolder.saved}
      <Banner kind="success">{t("syncFolder.savedNote")}</Banner>
    {/if}

    <div class="mt-5 flex gap-2.5">
      <Button variant="secondary" disabled={$syncFolder.busy} on:click={pickAndSave}>
        {$syncFolder.busy ? t("syncFolder.saving") : t("syncFolder.changeButton")}
      </Button>
    </div>
  </PanelCard>
{/if}
