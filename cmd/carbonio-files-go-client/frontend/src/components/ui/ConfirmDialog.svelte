<script>
  import { createEventDispatcher } from "svelte";
  import Button from "./Button.svelte";

  // Generic modal confirmation dialog: renders nothing while closed, and a
  // centered card over a dimmed backdrop while open. The caller owns the
  // open/closed state and reacts to the confirm/cancel events - this
  // component never calls any API itself.
  export let open = false;
  export let title = "";
  export let confirmLabel = "";
  export let cancelLabel = "";
  // Renders the confirm button in the danger (red) style for destructive
  // actions, matching Banner's/Button's "danger" color token.
  export let danger = false;
  // Disables both buttons and is meant to be set true while the caller's
  // confirm handler is awaiting its API call, so a slow request can't be
  // triggered twice or dismissed mid-flight.
  export let busy = false;

  const dispatch = createEventDispatcher();

  function cancel() {
    if (busy) return;
    dispatch("cancel");
  }

  function confirm() {
    if (busy) return;
    dispatch("confirm");
  }

  // Closes on a backdrop click, but not on a click that started inside the
  // card and was released over the backdrop (e.g. a text selection drag).
  function onBackdropMousedown(event) {
    if (event.target === event.currentTarget) cancel();
  }

  function onKeydown(event) {
    if (open && event.key === "Escape") cancel();
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    role="presentation"
    on:mousedown={onBackdropMousedown}
  >
    <div
      class="w-full max-w-md rounded border border-border bg-surface p-6 shadow-lg"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="confirm-dialog-title"
    >
      <h3 id="confirm-dialog-title" class="mb-2.5 mt-0 text-base font-semibold">{title}</h3>
      <div class="mb-5 text-sm text-muted">
        <slot />
      </div>
      <div class="flex justify-end gap-2.5">
        <Button variant="secondary" full={false} disabled={busy} on:click={cancel}>{cancelLabel}</Button>
        <Button variant={danger ? "danger" : "primary"} full={false} disabled={busy} on:click={confirm}>
          {confirmLabel}
        </Button>
      </div>
    </div>
  </div>
{/if}
