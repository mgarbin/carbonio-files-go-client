<script>
  import { docsViewer } from "../lib/stores";
  import { t } from "../lib/i18n";
  import * as api from "../lib/api";

  // Closes the embedded webview and returns to the Docs Online file
  // browser. The local proxy (see App.OpenNodeWithDocs) keeps running for
  // the rest of the session - it's cheap to leave up and lets reopening a
  // document skip straight back to loading its iframe.
  //
  // Also kicks off a full cache sync (UpdateCacheSync followed by
  // LiveCacheSync - see App.StartFullSync/actions.FullCacheSync): the
  // document may have just been edited in the embedded editor, so the
  // local sync cache/folder need to catch up with whatever changed
  // remotely. Checks GetSyncStatus first and skips straight past when a
  // sync (manual or the periodic background job) is already running -
  // App.StartFullSync would just reject it anyway (see tryBeginSync), so
  // this only avoids a pointless call and its console noise. Still a
  // fire-and-forget best effort: the button must stay instant, the
  // dashboard's sync status panel is where progress/errors are surfaced,
  // and remaining errors are only logged, never block returning to the
  // file browser.
  function close() {
    docsViewer.set(null);
    api
      .getSyncStatus()
      .then((status) => (status.inProgress ? null : api.startFullSync()))
      .catch((err) => console.error(err));
  }
</script>

<div class="flex h-full flex-col">
  <div class="flex flex-shrink-0 items-center gap-3 border-b border-border bg-surface px-4 py-2.5">
    <button
      type="button"
      class="rounded px-2.5 py-1.5 text-sm font-semibold text-brand hover:bg-bg"
      on:click={close}
    >
      &larr; {t("docsOnline.back")}
    </button>
    <span class="truncate text-sm font-semibold text-text">{$docsViewer?.name}</span>
  </div>
  <!-- The embedded webview: see App.OpenNodeWithDocs for why this points
       at a local reverse proxy (http://127.0.0.1:<port>/...) instead of
       the real https://<server>/... link - Wails v2 has no cookie-manager
       API, so the ZM_AUTH_TOKEN session cookie can't be seeded directly
       into this iframe's origin. -->
  <iframe title={$docsViewer?.name} src={$docsViewer?.url} class="w-full flex-1 border-0"></iframe>
</div>
