<script>
  import { onMount } from "svelte";
  import { booting, translations, view, session } from "./lib/stores";
  import { handleLoginResult } from "./lib/auth";
  import * as api from "./lib/api";
  import { t } from "./lib/i18n";

  import BootScreen from "./components/BootScreen.svelte";
  import LoginScreen from "./components/LoginScreen.svelte";
  import SyncSetupScreen from "./components/SyncSetupScreen.svelte";
  import DashboardShell from "./components/DashboardShell.svelte";

  onMount(async () => {
    try {
      const initial = await api.init();
      translations.set((initial && initial.translations) || {});
      document.documentElement.lang = (initial && initial.locale) || "en";
      document.title = t("app.title");
      booting.set(false);
      if (initial && initial.attemptedAutoLogin && initial.autoLogin) {
        handleLoginResult(initial.autoLogin);
      } else {
        view.set("login");
      }
    } catch (err) {
      console.error(err);
      translations.set({});
      booting.set(false);
      view.set("login");
    }
  });
</script>

<div class="h-full bg-bg text-text">
  {#if $booting}
    <BootScreen />
  {:else if $view === "sync-setup" && $session}
    <SyncSetupScreen />
  {:else if $view === "dashboard" && $session}
    <DashboardShell />
  {:else}
    <LoginScreen />
  {/if}
</div>
