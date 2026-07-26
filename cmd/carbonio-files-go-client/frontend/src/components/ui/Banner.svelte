<script>
  import { onDestroy } from "svelte";

  // 'error' | 'success' | 'warning' | 'status'
  export let kind = "error";

  // Success banners (e.g. "settings saved") are transient confirmations,
  // not something the user needs to dismiss - auto-hide them after 5s.
  // Reactive on `kind` (not onMount) so a Banner instance that's reused
  // across re-renders re-arms whenever it actually flips to "success"
  // (Svelte skips the rerun when the prop value hasn't changed).
  const AUTO_HIDE_MS = 5000;
  let visible = true;
  let timer;

  $: {
    clearTimeout(timer);
    visible = true;
    if (kind === "success") {
      timer = setTimeout(() => {
        visible = false;
      }, AUTO_HIDE_MS);
    }
  }

  onDestroy(() => clearTimeout(timer));
</script>

{#if visible}
  {#if kind === "error"}
    <div class="mb-4 rounded border border-danger-border bg-danger-bg px-3 py-2.5 text-sm text-danger">
      <slot />
    </div>
  {:else if kind === "success"}
    <div class="mb-4 rounded border border-success-border bg-success-bg px-3 py-2.5 text-sm text-success">
      <slot />
    </div>
  {:else if kind === "warning"}
    <div class="mb-4 rounded border border-warning-border bg-warning-bg px-3 py-2.5 text-sm text-warning">
      <slot />
    </div>
  {:else}
    <div class="py-5 text-center text-sm text-muted">
      <slot />
    </div>
  {/if}
{/if}
