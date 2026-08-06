// types/global.d.ts — ambient types for the extracted client scripts
// (assets/js/*.js). Lives outside assets/ deliberately: everything under
// assets/ is compiled into the release binary by `//go:embed all:assets`
// (see assets_embed_test.go) and served at /assets — this file is dev-time
// typing metadata, not something to ship or serve.
//
// Declares window.lilmailPush separately from push.js (rather than inline)
// because it is consumed from other, un-typechecked templates too
// (templates/settings.html calls window.lilmailPush.enable()/disable() from
// its own inline script, out of scope for this pass — see the extraction
// report).
export {};

/**
 * The 'lm.mail' Alpine store — layout/selection preferences persisted to
 * localStorage. Shape mirrors the object app.js registers via
 * Alpine.store('mail', ...).
 */
interface LilmailMailStore {
  pane: string;
  density: string;
  selection: string[];
  activeId: string | null;
  setPane(p: string): void;
  toggleDensity(): void;
  isSelected(id: string): boolean;
  toggleSelect(id: string): void;
  clearSelection(): void;
}

/** The 'ui' Alpine store — transient chrome state, not persisted. */
interface LilmailUiStore {
  sidebarOpen: boolean;
}

/** Payload HTMX passes on its 'htmx:configRequest' custom event. */
interface HtmxConfigRequestDetail {
  headers: Record<string, string>;
}

declare global {
  interface HTMLElementEventMap {
    // Only the HTMX custom events app.js listens for are declared — HTMX
    // fires many more that this project does not consume.
    'htmx:configRequest': CustomEvent<HtmxConfigRequestDetail>;
    'htmx:beforeRequest': CustomEvent;
    'htmx:afterRequest': CustomEvent;
  }

  interface Window {
    lilmailPush: {
      /** Requests Notification permission, then registers + subscribes. */
      enable: () => Promise<PushSubscriptionJSON>;
      /** Unsubscribes locally and tells the server to drop the subscription. */
      disable: () => Promise<void>;
      /** Whether this browser has the APIs push requires. */
      isSupported: () => boolean;
      /** Whether an active push subscription already exists. */
      isSubscribed: () => Promise<boolean>;
    };
  }

  // Alpine is loaded from assets/vendor/alpine.min.js (untyped, vendored — see
  // assetsAllowedUnreferenced / eslint ignores). This declares only the
  // handful of members app.js actually calls, not the full Alpine API.
  var Alpine: {
    store(name: 'ui', value: LilmailUiStore): void;
    store(name: 'mail', value: LilmailMailStore): void;
    store(name: 'mail'): LilmailMailStore;
    effect(fn: () => void): void;
  };
}
