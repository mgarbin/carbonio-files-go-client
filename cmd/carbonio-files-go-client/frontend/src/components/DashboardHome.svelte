<script>
  import { onMount, onDestroy } from "svelte";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import PanelCard from "./ui/PanelCard.svelte";
  import Banner from "./ui/Banner.svelte";
  import Button from "./ui/Button.svelte";
  import ConfirmDialog from "./ui/ConfirmDialog.svelte";

  // Polls the sync cache while the dashboard is open so the "missing
  // locally" counter and the in-progress badge stay live while a sync
  // (manual or the periodic background job) is actually running.
  let syncPollHandle;
  onMount(() => {
    loadSyncStatus();
    syncPollHandle = setInterval(refreshSyncStatus, 2000);
  });
  onDestroy(() => {
    if (syncPollHandle) clearInterval(syncPollHandle);
  });

  let syncStatus = null;
  let syncStatusLoading = true;
  let syncStatusError = null;

  function loadSyncStatus() {
    syncStatusLoading = true;
    syncStatusError = null;
    api
      .getSyncStatus()
      .then((status) => {
        syncStatus = status;
        syncStatusLoading = false;
      })
      .catch((err) => {
        syncStatusLoading = false;
        syncStatusError = "generic";
        console.error(err);
      });
  }

  // Same fetch as loadSyncStatus, but silent: used for the periodic poll
  // so live updates never flash the "Loading…" banner over the panel.
  function refreshSyncStatus() {
    api
      .getSyncStatus()
      .then((status) => {
        syncStatus = status;
      })
      .catch((err) => console.error(err));
  }

  // Uses the OS/browser locale and timezone of the machine running the
  // app, via Intl (Date.prototype.toLocaleString) - no hardcoded format.
  $: lastSyncedDisplay =
    syncStatus && syncStatus.lastSyncedAt ? new Date(syncStatus.lastSyncedAt).toLocaleString() : t("dashboard.syncNeverRun");

  let syncActionBusy = false;
  let syncActionError = null;

  // Persists the on/off decision (see App.SetSyncEnabled) so it survives
  // an app restart - the periodic background job resumes automatically on
  // the next login when left enabled (see maybeStartBackgroundSync). When
  // turning sync on, also kicks an immediate fullCacheSync run so the user
  // sees instant feedback instead of waiting for the next periodic tick.
  function toggleSync() {
    const enabling = !syncStatus?.enabled;
    syncActionBusy = true;
    syncActionError = null;
    api
      .setSyncEnabled(enabling)
      .then(() => (enabling ? api.startFullSync() : null))
      .then(() => {
        syncActionBusy = false;
        refreshSyncStatus();
      })
      .catch((err) => {
        syncActionBusy = false;
        syncActionError = "generic";
        console.error(err);
        refreshSyncStatus();
      });
  }

  let resetSyncDialogOpen = false;
  let resetSyncBusy = false;

  // Opens the "Reset sync" confirmation dialog (see ConfirmDialog below);
  // nothing is stopped or deleted until the user explicitly accepts it.
  function openResetSyncDialog() {
    syncActionError = null;
    resetSyncDialogOpen = true;
  }

  // Cancel just closes the dialog - no API call, no state changes.
  function cancelResetSync() {
    resetSyncDialogOpen = false;
  }

  // Accept: stops the sync process and permanently deletes the cached
  // sync data, including the last sync date (see App.ResetSync). Reuses
  // the same error banner as toggleSync so sync actions behave
  // consistently across the dashboard.
  function confirmResetSync() {
    resetSyncBusy = true;
    api
      .resetSync()
      .then(() => {
        resetSyncBusy = false;
        resetSyncDialogOpen = false;
        refreshSyncStatus();
      })
      .catch((err) => {
        resetSyncBusy = false;
        resetSyncDialogOpen = false;
        syncActionError = "generic";
        console.error(err);
        refreshSyncStatus();
      });
  }
</script>

<h2 class="mb-3 mt-0 text-xl font-semibold">{t("dashboard.welcomeTitle")}</h2>
<p class="mb-8 max-w-2xl text-sm text-text">{t("dashboard.description")}</p>

<h3 class="mb-2.5 text-xs font-bold uppercase tracking-wide text-muted">{t("dashboard.syncStatusTitle")}</h3>
<PanelCard maxWidth={true}>
  <div class="flex items-center justify-between py-1.5 text-sm">
    <span class="text-muted">{t("dashboard.syncStatusLabel")}</span>
    {#if syncStatus && syncStatus.inProgress}
      <span class="rounded-full bg-brand/[0.12] px-2.5 py-0.5 text-xs font-semibold text-brand-dark">
        {t("dashboard.syncStatusInProgress")}
      </span>
    {:else if syncStatus?.enabled}
      <span class="rounded-full bg-success-bg px-2.5 py-0.5 text-xs font-semibold text-success">
        {t("dashboard.syncStatusActive")}
      </span>
    {:else}
      <span class="rounded-full bg-danger-bg px-2.5 py-0.5 text-xs font-semibold text-danger">
        {t("dashboard.syncStatusInactive")}
      </span>
    {/if}
  </div>

  {#if syncStatusLoading}
    <Banner kind="status">{t("common.loading")}</Banner>
  {:else if syncStatusError}
    <Banner kind="error">{errorMessage(syncStatusError)}</Banner>
  {:else if syncStatus}
    <div class="flex items-center justify-between py-1.5 text-sm">
      <span class="text-muted">{t("dashboard.lastSyncLabel")}</span>
      <span class="font-semibold">{lastSyncedDisplay}</span>
    </div>
    <div class="flex items-center justify-between py-1.5 text-sm">
      <span class="text-muted">{t("dashboard.remoteItemsLabel")}</span>
      <span class="font-semibold">{syncStatus.remoteItems}</span>
    </div>
    <div class="flex items-center justify-between py-1.5 text-sm">
      <span class="text-muted">{t("dashboard.localItemsLabel")}</span>
      <span class="font-semibold">{syncStatus.localItems}</span>
    </div>
    <div class="flex items-center justify-between py-1.5 text-sm">
      <span class="text-muted">{t("dashboard.missingOnServerLabel")}</span>
      <span class="font-semibold">{syncStatus.missingOnServer}</span>
    </div>
    <div class="flex items-center justify-between py-1.5 text-sm">
      <span class="text-muted">{t("dashboard.missingLocallyLabel")}</span>
      <span class="font-semibold">{syncStatus.missingLocally}</span>
    </div>

    {#if syncStatus.lastError}
      <div class="mt-4 rounded border border-danger-border bg-danger-bg px-3 py-2.5 text-sm text-danger">
        <p class="mb-1 font-semibold">{t("dashboard.syncErrorsTitle")}</p>
        <p class="break-words">{syncStatus.lastError}</p>
      </div>
    {/if}
  {/if}
</PanelCard>

<h3 class="mb-2.5 mt-8 text-xs font-bold uppercase tracking-wide text-muted">{t("dashboard.actionsTitle")}</h3>
<div class="flex flex-wrap gap-2.5">
  <Button variant="primary" full={false} disabled={syncActionBusy || syncStatus?.inProgress} on:click={toggleSync}>
    {syncStatus?.enabled ? t("dashboard.stopSync") : t("dashboard.startSync")}
  </Button>
  <Button variant="secondary" full={false} on:click={openResetSyncDialog} disabled={syncActionBusy || syncStatus?.inProgress}>{t("dashboard.resetSync")}</Button>
</div>

{#if syncActionError}
  <div class="mt-2.5 max-w-2xl">
    <Banner kind="error">{errorMessage(syncActionError)}</Banner>
  </div>
{/if}

<ConfirmDialog
  open={resetSyncDialogOpen}
  busy={resetSyncBusy}
  danger={true}
  title={t("dashboard.resetSyncConfirmTitle")}
  confirmLabel={t("dashboard.resetSyncConfirmAccept")}
  cancelLabel={t("dashboard.resetSyncConfirmCancel")}
  on:confirm={confirmResetSync}
  on:cancel={cancelResetSync}
>
  {t("dashboard.resetSyncConfirmBody")}
</ConfirmDialog>
