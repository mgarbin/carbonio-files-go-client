<script>
  import { loginBusy, loginError, loginPrefill } from "../lib/stores";
  import { submitLogin } from "../lib/auth";
  import { t, errorMessage } from "../lib/i18n";
  import Banner from "./ui/Banner.svelte";
  import TextInput from "./ui/TextInput.svelte";
  import Button from "./ui/Button.svelte";

  // Seeded once from the last failed attempt (auto-login or manual); not
  // meant to react to further store changes while this form is mounted.
  let endpoint = $loginPrefill.endpoint;
  let username = $loginPrefill.username;
  let password = "";

  function onSubmit() {
    submitLogin(endpoint, username, password);
  }
</script>

<div class="flex h-full items-center justify-center p-6">
  <div class="w-full max-w-sm rounded border border-border bg-surface p-8 shadow-lg shadow-slate-950/10">
    <h1 class="mb-6 text-center text-xl font-bold">{t("login.heading")}</h1>

    {#if $loginError}
      <Banner kind="error">{errorMessage($loginError)}</Banner>
    {/if}

    <form on:submit|preventDefault={onSubmit}>
      <TextInput
        id="f-endpoint"
        label={t("login.endpointLabel")}
        placeholder={t("login.endpointPlaceholder")}
        bind:value={endpoint}
        disabled={$loginBusy}
      />
      <TextInput
        id="f-username"
        label={t("login.usernameLabel")}
        placeholder={t("login.usernamePlaceholder")}
        bind:value={username}
        disabled={$loginBusy}
      />
      <TextInput
        id="f-password"
        type="password"
        label={t("login.passwordLabel")}
        placeholder="••••••••"
        bind:value={password}
        disabled={$loginBusy}
      />
      <Button type="submit" disabled={$loginBusy}>{$loginBusy ? t("login.signingIn") : t("login.submit")}</Button>
    </form>

    <p class="mt-4 text-center text-xs leading-relaxed text-muted">{t("login.rememberNote")}</p>
  </div>
</div>
