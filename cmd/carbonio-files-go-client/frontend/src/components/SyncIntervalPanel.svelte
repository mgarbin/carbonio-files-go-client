<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { syncInterval } from "../lib/stores";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import Banner from "./ui/Banner.svelte";
  import PanelCard from "./ui/PanelCard.svelte";
  import SelectInput from "./ui/SelectInput.svelte";
  import Button from "./ui/Button.svelte";

  // Mirrors App.validSyncIntervalsMinutes: the only values the backend
  // accepts, 5 being the minimum and the default.
  const SYNC_INTERVALS_MINUTES = [5, 15, 30, 60];

  // Seed the form from whatever's already cached (revisiting the panel
  // after a first load); loadIfNeeded fills this in once the fetch
  // resolves when nothing was cached yet.
  const cached = get(syncInterval);
  let minutes = cached.minutes;

  onMount(loadIfNeeded);

  function loadIfNeeded() {
    const si = get(syncInterval);
    if (si.loaded || si.loading) return;
    syncInterval.update((s) => ({ ...s, loading: true }));
    api
      .getSyncIntervalMinutes()
      .then((value) => {
        const next = {
          loaded: true,
          loading: false,
          minutes: value || 5,
          busy: false,
          error: null,
          saved: false,
        };
        syncInterval.set(next);
        minutes = next.minutes;
      })
      .catch((err) => {
        syncInterval.update((s) => ({ ...s, loading: false, error: "generic" }));
        console.error(err);
      });
  }

  function save() {
    syncInterval.update((s) => ({ ...s, busy: true, error: null, saved: false }));
    api
      .setSyncIntervalMinutes(minutes)
      .then(() => {
        syncInterval.set({ loaded: true, loading: false, minutes, busy: false, error: null, saved: true });
      })
      .catch((err) => {
        syncInterval.update((s) => ({ ...s, busy: false, error: "generic" }));
        console.error(err);
      });
  }
</script>

<h2 class="mb-5 mt-0 text-xl font-semibold">{t("syncInterval.panelTitle")}</h2>

{#if $syncInterval.loading && !$syncInterval.loaded}
  <Banner kind="status">{t("common.loading")}</Banner>
{:else}
  <PanelCard>
    <form on:submit|preventDefault={save}>
      <SelectInput
        id="si-minutes"
        label={t("syncInterval.intervalLabel")}
        bind:value={minutes}
        disabled={$syncInterval.busy}
        options={SYNC_INTERVALS_MINUTES.map((m) => ({ value: m, label: t("syncInterval.minutes." + m) }))}
      />

      {#if $syncInterval.error}
        <Banner kind="error">{errorMessage($syncInterval.error)}</Banner>
      {/if}
      {#if $syncInterval.saved}
        <Banner kind="success">{t("syncInterval.savedNote")}</Banner>
      {/if}

      <div class="flex gap-2.5">
        <Button type="submit" full={false} disabled={$syncInterval.busy}>
          {$syncInterval.busy ? t("syncInterval.saving") : t("syncInterval.saveButton")}
        </Button>
      </div>
    </form>
  </PanelCard>
{/if}
