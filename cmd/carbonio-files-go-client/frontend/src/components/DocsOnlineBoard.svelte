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

  // Case-insensitive filter box; resets to page 1 whenever it changes so
  // paging never points past the end of a shorter filtered set.
  let searchQuery = "";
  let previousSearchQuery = "";

  onMount(loadIfNeeded);

  // Runs on every mount of this component - not just the very first time
  // the user opens Docs Online - so returning to this section later in
  // the session (after the periodic background sync job, or a manual
  // "Avvia Sincronizzazione", has since populated/changed the local sync
  // cache) reflects the current cache instead of the empty/stale tree
  // fetched the first time this section was opened. GetDocsOnlineTree
  // only reads the local sqlite cache (see its doc comment), so refetching
  // on every visit is cheap - no network round trip involved. Skips a
  // redundant call while one is already in flight. The already-loaded
  // tree stays on screen while a repeat fetch runs (the
  // `$docsOnline.loading && !$docsOnline.loaded` guard in the template
  // only shows the full-page loader on the very first load) and a repeat
  // fetch that fails leaves the previous tree in place instead of
  // replacing it with an error banner - only a first-load failure is
  // surfaced to the user; a later silent failure is just logged.
  function loadIfNeeded() {
    const state = get(docsOnline);
    if (state.loading) return;
    const firstLoad = !state.loaded;
    docsOnline.update((s) => ({ ...s, loading: true, error: null }));
    api
      .getDocsOnlineTree()
      .then((tree) => {
        docsOnline.update((s) => ({
          ...s,
          loaded: true,
          loading: false,
          tree,
          // The breadcrumb path holds node references from the previous
          // tree; re-resolve it against the freshly fetched one so a user
          // who was drilled into a subfolder doesn't keep pointing at a
          // detached, possibly out-of-date node object.
          path: resolvePath(s.path, tree),
        }));
      })
      .catch((err) => {
        docsOnline.update((s) => ({ ...s, loading: false, error: firstLoad ? "generic" : s.error }));
        console.error(err);
      });
  }

  // Walks oldPath's folder ids against newTree level by level, stopping
  // at the first id no longer found there (e.g. the folder was deleted,
  // or moved, remotely) - landing on the deepest ancestor that still
  // exists in the fresh tree instead of keeping a stale/dangling node.
  function resolvePath(oldPath, newTree) {
    const resolved = [];
    let node = newTree;
    for (const { id } of oldPath) {
      const match = node.children.find((c) => c.isFolder && c.id === id);
      if (!match) break;
      resolved.push(match);
      node = match;
    }
    return resolved;
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

  $: if (searchQuery !== previousSearchQuery) {
    previousSearchQuery = searchQuery;
    page = 0;
  }

  // Recursively walks `node` and its subfolders, returning every file
  // whose name matches `query` (case-insensitive) as a flat list - a
  // match found several folders down still shows up while browsing a
  // parent, without pulling any folder rows into the result.
  function collectMatchingFiles(node, query) {
    let matches = [];
    for (const child of node.children) {
      if (child.isFolder) {
        matches = matches.concat(collectMatchingFiles(child, query));
      } else if (child.name.toLowerCase().includes(query)) {
        matches.push(child);
      }
    }
    return matches;
  }

  // With no active query, show the folder as-is (files + folders). Once
  // a query is typed, results become the flat list of matching files
  // found anywhere under the current folder - folders are dropped
  // entirely so the list only ever shows matched file names.
  $: filteredChildren = currentFolder
    ? searchQuery.trim()
      ? collectMatchingFiles(currentFolder, searchQuery.trim().toLowerCase())
      : currentFolder.children
    : [];

  $: totalPages = currentFolder ? Math.max(1, Math.ceil(filteredChildren.length / PAGE_SIZE)) : 1;
  $: pageItems = filteredChildren.slice(page * PAGE_SIZE, page * PAGE_SIZE + PAGE_SIZE);

  function nextPage() {
    if (page < totalPages - 1) page += 1;
  }

  function prevPage() {
    if (page > 0) page -= 1;
  }

  function openFolder(node) {
    searchQuery = "";
    docsOnline.update((s) => ({ ...s, path: [...s.path, node] }));
  }

  function goToRoot() {
    searchQuery = "";
    docsOnline.update((s) => ({ ...s, path: [] }));
  }

  function goToCrumb(index) {
    searchQuery = "";
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
<Banner kind="warning">{t("docsOnline.unsupportedWarning")}</Banner>

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

  <div class="mb-3">
    <input
      type="search"
      class="w-full rounded border border-border bg-surface px-3 py-2.5 text-sm text-text focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/[0.15]"
      placeholder={t("docsOnline.searchPlaceholder")}
      aria-label={t("docsOnline.searchPlaceholder")}
      bind:value={searchQuery}
    />
  </div>

  {#if openError}
    <Banner kind="error">{errorMessage(openError)}</Banner>
  {/if}

  <PanelCard maxWidth={false}>
    {#if currentFolder.children.length === 0}
      <p class="py-5 text-center text-sm text-muted">{t("docsOnline.emptyNote")}</p>
    {:else if filteredChildren.length === 0}
      <p class="py-5 text-center text-sm text-muted">{t("docsOnline.noResults")}</p>
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
