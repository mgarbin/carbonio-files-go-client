<script>
  import { section } from "../lib/stores";
  import { t } from "../lib/i18n";
  import ThemeToggle from "./ThemeToggle.svelte";

  const prefItems = [
    { key: "authentication", label: () => t("menu.authentication") },
    { key: "syncFolder", label: () => t("menu.syncFolder") },
    { key: "logging", label: () => t("menu.logging") },
  ];

  function go(key) {
    section.set(key);
  }

  const itemClass = (active) =>
    "block w-full rounded px-2.5 py-2 text-left text-sm " +
    (active ? "bg-brand/[0.12] font-semibold text-brand-dark" : "text-text hover:bg-bg");
</script>

<div class="flex w-60 flex-shrink-0 flex-col border-r border-border bg-surface p-3">
  <button
    type="button"
    class="mb-1 block w-full rounded px-2.5 py-2.5 text-left text-base font-bold text-brand-dark transition-colors {$section === 'dashboard' ? 'bg-brand/[0.2]' : 'bg-brand/[0.12] hover:bg-brand/[0.2]'}"
    on:click={() => go("dashboard")}
  >
    {t("menu.dashboard")}
  </button>

  <div class="px-2.5 pb-1 pt-3.5 text-[11px] font-bold uppercase tracking-wide text-muted">{t("menu.preferences")}</div>
  <div class="ml-2.5 border-l-2 border-border pl-1">
    {#each prefItems as item (item.key)}
      <button type="button" class={itemClass($section === item.key)} on:click={() => go(item.key)}>
        {item.label()}
      </button>
    {/each}
  </div>

  <div class="mt-auto pt-4">
    <ThemeToggle />
  </div>
</div>
