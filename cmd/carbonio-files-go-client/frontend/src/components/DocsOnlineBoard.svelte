<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { docsOnline, docsViewer } from "../lib/stores";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import Banner from "./ui/Banner.svelte";
  import PanelCard from "./ui/PanelCard.svelte";
  import Button from "./ui/Button.svelte";
  import Loader from "./ui/Loader.svelte";

  // Rows are paginated client-side, PAGE_SIZE per page - the tree is
  // already fully loaded, so paging just slices the current folder's
  // (already filtered/sorted) children array.
  const PAGE_SIZE = 10;
  let page = 0;
  // Tracks which folder `page` belongs to, so navigating to a different
  // folder (or back up the breadcrumb) always lands on page 1 instead of
  // carrying over an out-of-range page index.
  let pagedFolderId = null;

  // id of the file currently being opened (drives its row's busy state),
  // or null when no "Open" call is in flight.
  let openingId = null;
  let openError = null;

  onMount(loadIfNeeded);

  function loadIfNeeded() {
    const state = get(docsOnline);
    if (state.loaded || state.loading) return;
    docsOnline.update((s) => ({ ...s, loading: true, error: null }));
    api
      .getDocsOnlineTree()
      .then((tree) => {
        docsOnline.update((s) => ({ ...s, loaded: true, loading: false, tree }));
      })
      .catch((err) => {
        docsOnline.update((s) => ({ ...s, loading: false, error: "generic" }));
        console.error(err);
      });
  }

  // The folder currently on screen: the last breadcrumb entry, or the
  // tree's root while path is empty.
  $: currentFolder = $docsOnline.path.length
    ? $docsOnline.path[$docsOnline.path.length - 1]
    : $docsOnline.tree;

  // Reset to page 1 whenever the folder on screen changes (navigating
  // into/out of a folder), instead of carrying over a page index that
  // may no longer exist in the new folder.
  $: if (currentFolder && currentFolder.id !== pagedFolderId) {
    pagedFolderId = currentFolder.id;
    page = 0;
  }

  $: totalPages = currentFolder ? Math.max(1, Math.ceil(currentFolder.children.length / PAGE_SIZE)) : 1;
  $: pageItems = currentFolder ? currentFolder.children.slice(page * PAGE_SIZE, page * PAGE_SIZE + PAGE_SIZE) : [];

  function nextPage() {
    if (page < totalPages - 1) page += 1;
  }

  function prevPage() {
    if (page > 0) page -= 1;
  }

  function openFolder(node) {
    docsOnline.update((s) => ({ ...s, path: [...s.path, node] }));
  }

  function goToRoot() {
    docsOnline.update((s) => ({ ...s, path: [] }));
  }

  function goToCrumb(index) {
    docsOnline.update((s) => ({ ...s, path: s.path.slice(0, index + 1) }));
  }

  function openWithDocs(node) {
    if (openingId) return;
    openingId = node.id;
    openError = null;
    api
      .openNodeWithDocs(node.id)
      .then((url) => {
        docsViewer.set({ nodeId: node.id, name: node.name, url });
      })
      .catch((err) => {
        openError = "generic";
        console.error(err);
      })
      .finally(() => {
        openingId = null;
      });
  }
</script>

<h2 class="mb-3 mt-0 text-xl font-semibold">{t("docsOnline.title")}</h2>
<p class="mb-5 max-w-2xl text-sm text-muted">{t("docsOnline.description")}</p>

{#if $docsOnline.loading && !$docsOnline.loaded}
  <Loader message={t("common.loading")} />
{:else if $docsOnline.error}
  <Banner kind="error">{errorMessage($docsOnline.error)}</Banner>
{:else if currentFolder}
  <nav class="mb-3 flex flex-wrap items-center gap-1 text-sm">
    <button
      type="button"
      class="rounded px-1.5 py-1 {$docsOnline.path.length === 0
        ? 'font-semibold text-text'
        : 'text-brand hover:underline'}"
      disabled={$docsOnline.path.length === 0}
      on:click={goToRoot}
    >
      {t("docsOnline.rootLabel")}
    </button>
    {#each $docsOnline.path as crumb, index (crumb.id)}
      <span class="text-muted">/</span>
      <button
        type="button"
        class="rounded px-1.5 py-1 {index === $docsOnline.path.length - 1
          ? 'font-semibold text-text'
          : 'text-brand hover:underline'}"
        disabled={index === $docsOnline.path.length - 1}
        on:click={() => goToCrumb(index)}
      >
        {crumb.name}
      </button>
    {/each}
  </nav>

  {#if openError}
    <Banner kind="error">{errorMessage(openError)}</Banner>
  {/if}

  <PanelCard maxWidth={false}>
    {#if currentFolder.children.length === 0}
      <p class="py-5 text-center text-sm text-muted">{t("docsOnline.emptyNote")}</p>
    {:else}
      <ul class="divide-y divide-border">
        {#each pageItems as node (node.id)}
          <li class="flex items-center justify-between gap-3 py-2.5 text-sm">
            {#if node.isFolder}
              <button
                type="button"
                class="flex flex-1 items-center gap-2 rounded px-1.5 py-1 text-left hover:bg-bg"
                on:click={() => openFolder(node)}
              >
                <span class="font-semibold text-text">{node.name}</span>
              </button>
              <span class="text-muted">›</span>
            {:else}
              <span class="flex-1 truncate px-1.5 text-text">{node.name}</span>
              <Button
                variant="secondary"
                full={false}
                disabled={openingId === node.id}
                on:click={() => openWithDocs(node)}
              >
                {openingId === node.id ? t("docsOnline.opening") : t("docsOnline.openButton")}
              </Button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </PanelCard>

  {#if totalPages > 1}
    <div class="mt-3 flex items-center justify-between text-sm">
      <Button variant="secondary" full={false} disabled={page === 0} on:click={prevPage}>
        {t("docsOnline.prevPage")}
      </Button>
      <span class="text-muted">{t("docsOnline.pageLabel")} {page + 1}/{totalPages}</span>
      <Button variant="secondary" full={false} disabled={page >= totalPages - 1} on:click={nextPage}>
        {t("docsOnline.nextPage")}
      </Button>
    </div>
  {/if}
{/if}
