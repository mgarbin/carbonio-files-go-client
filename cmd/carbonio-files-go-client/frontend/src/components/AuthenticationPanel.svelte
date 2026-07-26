<script>
  import { session } from "../lib/stores";
  import { logout } from "../lib/auth";
  import { t, errorMessage } from "../lib/i18n";
  import * as api from "../lib/api";
  import Banner from "./ui/Banner.svelte";
  import TextInput from "./ui/TextInput.svelte";
  import Button from "./ui/Button.svelte";
  import PanelCard from "./ui/PanelCard.svelte";

  // Captured when the panel mounts (i.e. every time the user navigates
  // here - DashboardShell recreates this component on each section
  // switch) and reset back to the fresh session on a successful save.
  let baseline = currentSessionSnapshot();

  let endpoint = baseline.endpoint;
  let username = baseline.username;
  let password = "";

  let testing = false;
  let saving = false;
  // Outcome of the last Test click: null, or { snapshot, success, text }.
  // Deliberately never mutated in a reactive statement - `testMatchesForm`
  // below derives whether it's still relevant, avoiding a Test <-> Save
  // circular reactive dependency.
  let testResult = null;

  function currentSessionSnapshot() {
    const s = $session || { endpoint: "", username: "" };
    return { endpoint: s.endpoint, username: s.username, password: "" };
  }

  $: busy = testing || saving;
  $: dirty = endpoint !== baseline.endpoint || username !== baseline.username || password !== baseline.password;
  // A stale test result (pass or fail) no longer describes what's in the
  // form once any field has since changed.
  $: testMatchesForm =
    !!testResult &&
    endpoint === testResult.snapshot.endpoint &&
    username === testResult.snapshot.username &&
    password === testResult.snapshot.password;
  $: testDisabled = busy || !dirty;
  // Save only ever unlocks once Test has succeeded for exactly the values
  // currently in the form.
  $: saveDisabled = busy || !(testMatchesForm && testResult.success);
  $: message = testing
    ? { kind: "status", text: t("auth.testing") }
    : saving
      ? { kind: "status", text: t("auth.saving") }
      : testMatchesForm
        ? { kind: testResult.success ? "success" : "error", text: testResult.text }
        : null;

  function runTest() {
    const snapshot = { endpoint, username, password };
    testing = true;
    api
      .testLogin(snapshot.endpoint, snapshot.username, snapshot.password)
      .then((result) => {
        testing = false;
        if (result && result.success) {
          testResult = { snapshot, success: true, text: t("auth.testSuccess") };
        } else {
          testResult = { snapshot, success: false, text: errorMessage((result && result.errorKind) || "unknown") };
        }
      })
      .catch((err) => {
        testing = false;
        testResult = { snapshot, success: false, text: errorMessage("generic") };
        console.error(err);
      });
  }

  function runSave() {
    if (!(testMatchesForm && testResult.success)) return;
    const snapshot = { endpoint, username, password };
    saving = true;
    api
      .login(snapshot.endpoint, snapshot.username, snapshot.password)
      .then((result) => {
        saving = false;
        if (result && result.success) {
          session.set({ endpoint: result.endpoint, username: result.username });
          baseline = currentSessionSnapshot();
          endpoint = baseline.endpoint;
          username = baseline.username;
          password = "";
          testResult = null;
        } else {
          testResult = { snapshot, success: false, text: errorMessage((result && result.errorKind) || "unknown") };
        }
      })
      .catch((err) => {
        saving = false;
        testResult = { snapshot, success: false, text: errorMessage("generic") };
        console.error(err);
      });
  }

  function confirmLogout() {
    if (window.confirm(t("auth.logoutConfirm"))) {
      logout();
    }
  }
</script>

<h2 class="mb-5 mt-0 text-xl font-semibold">{t("auth.panelTitle")}</h2>

<PanelCard>
  <TextInput id="af-endpoint" label={t("auth.serverLabel")} bind:value={endpoint} disabled={busy} />
  <TextInput id="af-username" label={t("auth.usernameLabel")} bind:value={username} disabled={busy} />
  <TextInput
    id="af-password"
    type="password"
    label={t("auth.passwordLabel")}
    placeholder={t("auth.passwordPlaceholder")}
    bind:value={password}
    disabled={busy}
  />

  {#if message}
    {#if message.kind === "status"}
      <p class="py-1 text-sm text-muted">{message.text}</p>
    {:else}
      <Banner kind={message.kind}>{message.text}</Banner>
    {/if}
  {/if}

  <div class="mt-5 flex gap-2.5">
    <Button variant="secondary" disabled={testDisabled} on:click={runTest}>{t("auth.testButton")}</Button>
    <Button full={false} disabled={saveDisabled} on:click={runSave}>{t("auth.saveButton")}</Button>
  </div>
</PanelCard>

<div class="mt-6">
  <PanelCard>
    <div class="flex gap-2.5">
      <Button variant="secondary" on:click={confirmLogout}>{t("auth.logout")}</Button>
    </div>
  </PanelCard>
</div>
