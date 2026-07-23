<script>
  // Message shown under the spinner (e.g. a localized status string).
  export let message = "";
</script>

<div class="flex h-full items-center justify-center p-6">
  <div class="flex flex-col items-center gap-4 text-center">
    <div class="h-10 w-10 animate-spin rounded-full border-4 border-border border-t-brand" />
    <p class="max-w-xs text-sm text-muted">{message}</p>
  </div>
</div>
