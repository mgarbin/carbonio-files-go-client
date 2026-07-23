<script>
  // 'error' | 'success' | 'status'
  export let kind = "error";
</script>

{#if kind === "error"}
  <div class="mb-4 rounded border border-danger-border bg-danger-bg px-3 py-2.5 text-sm text-danger">
    <slot />
  </div>
{:else if kind === "success"}
  <div class="mb-4 rounded border border-success-border bg-success-bg px-3 py-2.5 text-sm text-success">
    <slot />
  </div>
{:else}
  <div class="py-5 text-center text-sm text-muted">
    <slot />
  </div>
{/if}
