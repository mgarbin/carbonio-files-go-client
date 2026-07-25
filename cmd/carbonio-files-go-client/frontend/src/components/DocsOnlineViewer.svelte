<script>
  import { docsViewer } from "../lib/stores";
  import { t } from "../lib/i18n";

  // Closes the embedded webview and returns to the Docs Online file
  // browser. The local proxy (see App.OpenNodeWithDocs) keeps running for
  // the rest of the session - it's cheap to leave up and lets reopening a
  // document skip straight back to loading its iframe.
  function close() {
    docsViewer.set(null);
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
