// A stable per-tab identifier. It lets the server echo the originating tab on
// mutation events so that tab can ignore its own event (it already applied the
// change locally) while every other tab still invalidates its caches.
export const CLIENT_ID = (() => {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `tab-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`;
})();
