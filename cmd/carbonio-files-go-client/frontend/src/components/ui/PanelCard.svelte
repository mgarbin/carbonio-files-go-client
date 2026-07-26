<script>
  export let maxWidth = true;
</script>

<div class="rounded border border-border bg-surface p-6 {maxWidth ? 'max-w-[560px]' : ''}">
  <slot />
</div>
