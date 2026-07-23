<script>
  import { section } from "../lib/stores";
  import Sidebar from "./Sidebar.svelte";
  import DashboardHome from "./DashboardHome.svelte";
  import AuthenticationPanel from "./AuthenticationPanel.svelte";
  import SyncFolderPanel from "./SyncFolderPanel.svelte";
  import LoggingPanel from "./LoggingPanel.svelte";
</script>

<div class="flex h-full">
  <Sidebar />
  <div class="flex-1 overflow-auto p-8 sm:p-10">
    {#if $section === "authentication"}
      <AuthenticationPanel />
    {:else if $section === "syncFolder"}
      <SyncFolderPanel />
    {:else if $section === "logging"}
      <LoggingPanel />
    {:else}
      <DashboardHome />
    {/if}
  </div>
</div>
