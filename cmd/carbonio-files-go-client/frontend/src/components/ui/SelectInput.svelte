<script>
  export let id;
  export let label = "";
  export let value;
  export let options = []; // [{ value, label }]
  export let disabled = false;
</script>

<div class="mb-4">
  {#if label}
    <label for={id} class="mb-1.5 block text-[13px] font-semibold text-muted">{label}</label>
  {/if}
  <select
    {id}
    {disabled}
    bind:value
    class="w-full rounded border border-border bg-surface px-3 py-2.5 text-sm text-text focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/[0.15]"
  >
    {#each options as opt (opt.value)}
      <option value={opt.value}>{opt.label}</option>
    {/each}
  </select>
</div>
