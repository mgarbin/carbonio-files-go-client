<script>
  import { themePreference, setTheme } from "../lib/theme";
  import { t } from "../lib/i18n";

  const options = [
    { value: "light", label: () => t("theme.light") },
    { value: "dark", label: () => t("theme.dark") },
    { value: "system", label: () => t("theme.system") },
  ];
</script>

<div>
  <div class="px-2.5 pb-1.5 text-[11px] font-bold uppercase tracking-wide text-muted">{t("theme.label")}</div>
  <div class="flex gap-1 rounded border border-border bg-bg p-1">
    {#each options as opt (opt.value)}
      <button
        type="button"
        class="flex-1 rounded px-2 py-1.5 text-xs font-semibold transition-colors {$themePreference === opt.value
          ? 'bg-surface text-brand-dark shadow-sm'
          : 'text-muted hover:text-text'}"
        on:click={() => setTheme(opt.value)}
      >
        {opt.label()}
      </button>
    {/each}
  </div>
</div>
