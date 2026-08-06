// assets/js/push.js — LilMail Web Push: service worker registration +
// subscription management.
//
// Called from the Settings page "Enable push notifications" button as
// window.lilmailPush.enable(), or from a prior subscription stored in the SW.
//
// Extracted verbatim from an inline <script> in main.html: this block reads
// its auth token from the '#app-token' DOM element's data-token attribute
// (rendered by the server as <span id="app-token" data-token="{{.Token}}">),
// not from an interpolated Go template value, so it moves to a static file
// unchanged.
'use strict';

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

(function () {
    /**
     * Reads the bearer token carried in the DOM for JS fetch calls.
     * @returns {string}
     */
    function currentToken() {
        const el = document.getElementById('app-token');
        return el instanceof HTMLElement && el.dataset.token ? el.dataset.token : '';
    }

    /**
     * urlBase64ToUint8Array converts a base64url string to a Uint8Array for
     * use as the applicationServerKey in PushManager.subscribe().
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

    /**
     * registerAndSubscribe registers the service worker and subscribes for
     * push. Returns a Promise that resolves with the subscription JSON on
     * success.
     * @returns {Promise<PushSubscriptionJSON>}
     */
    function registerAndSubscribe() {
        if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
            return Promise.reject(new Error('Push not supported in this browser'));
        }
        return navigator.serviceWorker.register('/sw.js', { scope: '/' })
            .then(function (reg) {
                // Fetch VAPID public key.
                return fetch('/api/push/vapid-public')
                    .then(function (r) { return r.json(); })
                    .then(function (data) {
                        if (!isVapidPublicKeyResponse(data)) {
                            throw new Error('Malformed VAPID public key response');
                        }
                        const key = urlBase64ToUint8Array(data.publicKey);
                        return reg.pushManager.subscribe({
                            userVisibleOnly: true,
                            applicationServerKey: key
                        });
                    });
            })
            .then(function (sub) {
                const subJson = sub.toJSON();
                // POST the subscription to the server.
                return fetch('/api/push/subscribe', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': 'Bearer ' + currentToken()
                    },
                    body: JSON.stringify(subJson)
                }).then(function (r) {
                    if (!r.ok) throw new Error('Server rejected subscription');
                    return subJson;
                });
            });
    }

    /**
     * unsubscribe removes the subscription both from the browser and the
     * server.
     * @returns {Promise<void>}
     */
    function unsubscribe() {
        if (!('serviceWorker' in navigator)) return Promise.resolve();
        return navigator.serviceWorker.ready
            .then(function (reg) { return reg.pushManager.getSubscription(); })
            .then(function (sub) {
                if (!sub) return;
                const endpoint = sub.endpoint;
                return sub.unsubscribe().then(function () {
                    return fetch('/api/push/subscribe', {
                        method: 'DELETE',
                        headers: {
                            'Content-Type': 'application/json',
                            'Authorization': 'Bearer ' + currentToken()
                        },
                        body: JSON.stringify({ endpoint: endpoint })
                    }).then(function () { /* server response not needed by callers */ });
                });
            });
    }

    // Expose public API for the Settings page.
    window.lilmailPush = {
        enable: function () {
            return Notification.requestPermission().then(function (perm) {
                if (perm !== 'granted') throw new Error('Notification permission denied');
                return registerAndSubscribe();
            });
        },
        disable: unsubscribe,
        isSupported: function () {
            return ('serviceWorker' in navigator) && ('PushManager' in window) && ('Notification' in window);
        },
        isSubscribed: function () {
            if (!('serviceWorker' in navigator)) return Promise.resolve(false);
            return navigator.serviceWorker.ready
                .then(function (reg) { return reg.pushManager.getSubscription(); })
                .then(function (sub) { return sub !== null; });
        }
    };

    // Auto-register the service worker on page load so push events are received
    // even without the Settings page (the subscription was already persisted).
    // Fire-and-forget: registration failures here have nothing to surface to —
    // there is no UI on screen yet to show an error in.
    if ('serviceWorker' in navigator) {
        void navigator.serviceWorker.register('/sw.js', { scope: '/' });
    }
})();
