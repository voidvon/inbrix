// sw.js — LilMail Service Worker
//
// Responsibilities:
//   1. Handle 'push' events: decrypt the JSON payload and show a notification.
//   2. Handle 'notificationclick': focus an existing LilMail tab or open a new one.
//   3. Handle 'pushsubscriptionchange': re-subscribe and POST the new subscription.
//
// This file is intentionally kept minimal — no caching / offline logic.
// It is registered by the inline script in layouts/main.html when Web Push is
// enabled (webPushEnabled flag injected by the template).

'use strict';

const LILMAIL_ORIGIN = self.location.origin;

// ── Push event ──────────────────────────────────────────────────────────────
//
// Bare `addEventListener`/`registration`/`clients` globals are used rather
// than `self.addEventListener` etc.: they are the exact same objects at
// runtime (both are properties of the same ServiceWorkerGlobalScope), but
// @types/serviceworker's ambient `self` is typed as the generic
// WorkerGlobalScope (shared with dedicated/shared workers) so member access
// through it loses the ServiceWorker-specific event map, while the bare
// globals are typed against ServiceWorkerGlobalScope directly.

addEventListener('push', function (event) {
    let data = { from: 'Unknown sender', subject: '(no subject)', tag: 'newmail' };
    if (event.data) {
        try { data = event.data.json(); } catch (_) { /* ignore */ }
    }

    const title = 'New mail from ' + (data.from || 'unknown');
    const options = {
        body: data.subject || '(no subject)',
        icon: '/assets/icon.png',
        badge: '/assets/icon.png',
        tag: data.tag || 'newmail',          // collapse multiple notifications
        renotify: false,
        data: { url: LILMAIL_ORIGIN + '/inbox' },
    };

    event.waitUntil(registration.showNotification(title, options));
});

// ── Notification click ───────────────────────────────────────────────────────

addEventListener('notificationclick', function (event) {
    event.notification.close();
    const targetUrl = (event.notification.data && event.notification.data.url)
        ? event.notification.data.url
        : LILMAIL_ORIGIN + '/inbox';

    event.waitUntil(
        clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (windowClients) {
            // Focus an existing LilMail tab if one is open.
            for (var i = 0; i < windowClients.length; i++) {
                var client = windowClients[i];
                if (client.url.startsWith(LILMAIL_ORIGIN) && 'focus' in client) {
                    return client.focus();
                }
            }
            // Otherwise open a new tab.
            if (clients.openWindow) {
                return clients.openWindow(targetUrl);
            }
        })
    );
});

// ── Subscription change ──────────────────────────────────────────────────────
// Fired by the browser when a push subscription is automatically refreshed
// (e.g. after a browser update).  We re-subscribe and POST the new subscription
// to the server so push delivery isn't interrupted.
//
// event.oldSubscription is nullable per spec (MDN: "the browser may not
// always be able to provide [it]"), but was read unconditionally as
// event.oldSubscription.options — a TypeError thrown synchronously, before
// event.waitUntil() runs, silently abandoning the resubscribe attempt and
// leaving the user unknowingly unsubscribed from push until they revisit
// Settings. Genuine bug, not a missing annotation: strict typing turned a
// silent runtime crash into a build-time error (TS18047). Fixed by falling
// back to a fresh subscription (same shape as push.js's registerAndSubscribe)
// when no prior options are available.

/**
 * urlBase64ToUint8Array converts a base64url string to a Uint8Array for use
 * as the applicationServerKey in PushManager.subscribe(). Duplicated from
 * assets/js/push.js: this file has no bundler/shared-module mechanism to
 * import it from, and the app intentionally ships with no bundler.
 * @param {string} base64String
 * @returns {Uint8Array<ArrayBuffer>}
 */
function urlBase64ToUint8Array(base64String) {
    var padding = '='.repeat((4 - base64String.length % 4) % 4);
    var base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
    var rawData = atob(base64);
    var outputArray = new Uint8Array(rawData.length);
    for (var i = 0; i < rawData.length; ++i) {
        outputArray[i] = rawData.charCodeAt(i);
    }
    return outputArray;
}

addEventListener('pushsubscriptionchange', function (event) {
    /** @type {Promise<PushSubscriptionOptionsInit>} */
    var options = event.oldSubscription
        ? Promise.resolve(event.oldSubscription.options)
        : fetch('/api/push/vapid-public')
            .then(function (r) { return r.json(); })
            .then(function (data) {
                return {
                    userVisibleOnly: true,
                    applicationServerKey: urlBase64ToUint8Array(data.publicKey)
                };
            });

    event.waitUntil(
        options
            .then(function (opts) { return registration.pushManager.subscribe(opts); })
            .then(function (newSub) {
                return fetch('/api/push/subscribe', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(newSub.toJSON()),
                });
            })
    );
});
