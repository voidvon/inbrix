// sw.js — LilMail Service Worker
//
// Responsibilities:
//   1. Handle 'push' events: decrypt the JSON payload and show a notification.
//   2. Handle 'notificationclick': focus an existing LilMail tab or open a new one.
//   3. Handle 'pushsubscriptionchange': re-subscribe and POST the new subscription.
//
// This file is intentionally kept minimal — no caching / offline logic. The
// React settings screen registers it when Web Push is enabled by the user.

'use strict';

const LILMAIL_ORIGIN = self.location.origin;
const SW_LOCALE = (new URL(self.location.href).searchParams.get('locale') || navigator.language || '').toLowerCase().startsWith('zh') ? 'zh-CN' : 'en';
/** @type {Record<string, string>} */
const SW_TRANSLATIONS = {
    'New mail from': '新邮件，发件人：',
    'unknown': '未知发件人',
    '(no subject)': '（无主题）',
    'Malformed VAPID public key response': 'VAPID 公钥响应格式错误',
};

/**
 * @param {string} key
 * @returns {string}
 */
function swT(key) {
    return SW_LOCALE === 'zh-CN' ? (SW_TRANSLATIONS[key] || key) : key;
}

/**
 * @typedef {Object} PushPayload
 * @property {string} [from]
 * @property {string} [subject]
 * @property {string} [tag]
 */

/**
 * PushMessageData.json() and Notification.data are both typed `any` by
 * spec — a JSDoc cast on either would satisfy tsc (which lets any/unknown
 * into anything silently) but not typescript-eslint's no-unsafe-* rules,
 * which track whether a value was genuinely checked. These two guards do
 * that check; a malformed payload falls back to the caller's defaults
 * instead of crashing the handler.
 * @param {unknown} v
 * @returns {v is PushPayload}
 */
function isPushPayload(v) {
    return typeof v === 'object' && v !== null;
}

/**
 * @param {unknown} v
 * @returns {v is { url?: string }}
 */
function isUrlBag(v) {
    return typeof v === 'object' && v !== null;
}

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
    /** @type {PushPayload} */
    let data = { from: swT('unknown'), subject: swT('(no subject)'), tag: 'newmail' };
    if (event.data) {
        try {
            /** @type {unknown} */
            const parsed = event.data.json();
            if (isPushPayload(parsed)) data = parsed;
        } catch {
            // Malformed payload — keep the defaults above.
        }
    }

    const title = swT('New mail from') + ' ' + (data.from || swT('unknown'));
    const options = {
        body: data.subject || swT('(no subject)'),
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
    /** @type {unknown} */
    const notifData = event.notification.data;
    const targetUrl = (isUrlBag(notifData) && typeof notifData.url === 'string')
        ? notifData.url
        : LILMAIL_ORIGIN + '/inbox';

    event.waitUntil(
        clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (windowClients) {
            // Focus an existing LilMail tab if one is open.
            for (let i = 0; i < windowClients.length; i++) {
                const client = windowClients[i];
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
 * @typedef {Object} VapidPublicKeyResponse
 * @property {string} publicKey
 */

/**
 * @param {unknown} v
 * @returns {v is VapidPublicKeyResponse}
 */
function isVapidPublicKeyResponse(v) {
    return typeof v === 'object' && v !== null && typeof (/** @type {{ publicKey?: unknown }} */ (v)).publicKey === 'string';
}

/**
 * urlBase64ToUint8Array converts a base64url string to a Uint8Array for use
 * as the applicationServerKey in PushManager.subscribe(). This service worker
 * is a standalone browser asset, so it cannot import the React module helper.
 * @param {string} base64String
 * @returns {Uint8Array<ArrayBuffer>}
 */
function urlBase64ToUint8Array(base64String) {
    const padding = '='.repeat((4 - base64String.length % 4) % 4);
    const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
    const rawData = atob(base64);
    const outputArray = new Uint8Array(rawData.length);
    for (let i = 0; i < rawData.length; ++i) {
        outputArray[i] = rawData.charCodeAt(i);
    }
    return outputArray;
}

addEventListener('pushsubscriptionchange', function (event) {
    /** @type {Promise<PushSubscriptionOptionsInit>} */
    const options = event.oldSubscription
        ? Promise.resolve(event.oldSubscription.options)
        : fetch('/api/push/vapid-public')
            .then(function (r) { return r.json(); })
            .then(function (data) {
                if (!isVapidPublicKeyResponse(data)) {
                    throw new Error(swT('Malformed VAPID public key response'));
                }
                return {
                    userVisibleOnly: true,
                    applicationServerKey: urlBase64ToUint8Array(data.publicKey)
                };
            });

    event.waitUntil(
        options
            .then(function (opts) { return registration.pushManager.subscribe(opts); })
            .then(function (newSub) {
                return fetch('/api/csrf', { credentials: 'include' })
                    .then(function (r) { return r.json(); })
                    .then(function (csrf) {
                        if (!csrf || typeof csrf.token !== 'string' || csrf.token === '') {
                            throw new Error('CSRF token unavailable');
                        }
                        return fetch('/api/push/subscribe', {
                            method: 'POST',
                            credentials: 'include',
                            headers: {
                                'Content-Type': 'application/json',
                                'X-CSRF-Token': csrf.token,
                            },
                            body: JSON.stringify(newSub.toJSON()),
                        });
                    });
            })
    );
});
