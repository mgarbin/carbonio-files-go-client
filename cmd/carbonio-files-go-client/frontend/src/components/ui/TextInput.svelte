<script>
  export let id;
  export let label = "";
  export let type = "text"; // 'text' | 'password'
  export let value = "";
  export let placeholder = "";
  export let disabled = false;

  const inputClass =
    "w-full rounded border border-border bg-surface px-3 py-2.5 text-sm text-text focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/[0.15] disabled:opacity-60";
</script>

<div class="mb-4">
  {#if label}
    <label for={id} class="mb-1.5 block text-[13px] font-semibold text-muted">{label}</label>
  {/if}
  <!-- Svelte requires a literal `type` on a two-way-bound <input>, so the
       text/password variants are two branches instead of a dynamic attr. -->
  {#if type === "password"}
    <input {id} type="password" {placeholder} {disabled} bind:value on:input class={inputClass} />
  {:else}
    <input {id} type="text" {placeholder} {disabled} bind:value on:input class={inputClass} />
  {/if}
</div>
