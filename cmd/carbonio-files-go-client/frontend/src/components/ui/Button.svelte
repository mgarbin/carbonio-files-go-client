<script>
  // 'primary' | 'secondary' | 'danger'
  export let variant = "primary";
  export let type = "button";
  export let disabled = false;
  // Mirrors the original .btn-primary { width: 100% } default, overridden
  // to auto-width for buttons living in an actions row (see callers).
  // Secondary buttons are always auto-width, matching the original CSS.
  export let full = true;
</script>

<button
  {type}
  {disabled}
  on:click
  class="rounded px-3.5 py-2.5 text-sm font-semibold transition-colors disabled:cursor-default disabled:opacity-60 {variant ===
  'primary'
    ? (full ? 'w-full' : 'w-auto') + ' bg-brand text-white hover:bg-brand-dark'
    : variant === 'danger'
      ? (full ? 'w-full' : 'w-auto') + ' bg-danger text-white hover:opacity-90'
      : 'w-auto border border-border bg-surface text-text hover:border-danger hover:text-danger'}"
>
  <slot />
</button>
