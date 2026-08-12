// assets/js/notifications.js — LilMail live-mail notifications.
//
// Opens an SSE connection to /events and, when the browser Notification API is
// both present and permitted, raises a desktop notification for each new-mail
// event. Loaded only when `notificationsEnabled` is true and the viewer is
// signed in (see templates/layouts/main.html).
//
// Extracted verbatim from an inline <script> in main.html: this block
// interpolates no Go template values, so it moves to a static file unchanged.
'use strict';

/**
 * @typedef {Object} MailEventPayload
 * @property {string} [from]
 * @property {string} [subject]
 */

// Runtime guard for a JSON.parse() result, narrowing `unknown` to
// MailEventPayload. A JSDoc type-cast on the parsed value would satisfy tsc
// (which allows any/unknown into any type silently) but not
// typescript-eslint's no-unsafe-* rules, which track whether a value was
// genuinely checked rather than merely asserted — a real distinction, since
// a malformed or unexpected /events payload should not crash this handler.
/**
 * @param {unknown} v
 * @returns {v is MailEventPayload}
 */
function isMailEventPayload(v) {
    return typeof v === 'object' && v !== null;
}

/**
 * @param {string} raw
 * @returns {unknown}
 */
function tryParseJSON(raw) {
    try {
        return JSON.parse(raw);
    } catch {
        return undefined;
    }
}

(function () {
    /** @type {boolean} Whether the Notification API exists in this browser. */
    const notificationSupported = typeof Notification !== 'undefined';
    const translate = window.lmT || function (key) { return key; };

    function startSSE() {
        if (!window.EventSource) return;
        const es = new EventSource('/events');
        es.addEventListener('message', function (e) {
            // EventSource message data is always a string (SSE is a text
            // protocol), but e.data's declared type is `any` (MessageEvent's
            // generic default) — going through `unknown` and a typeof guard
            // narrows it for real, rather than merely asserting it.
            /** @type {unknown} */
            const raw = e.data;
            if (typeof raw !== 'string') return;

            const parsed = tryParseJSON(raw);
            if (!isMailEventPayload(parsed)) return;

            // Guard on notificationSupported, not just permission: referencing
            // `Notification` when the API does not exist throws a
            // ReferenceError, which this branch is reached precisely when the
            // API is absent (see the fallback call to startSSE() below).
            if (notificationSupported && Notification.permission === 'granted') {
                new Notification(translate('New mail from') + ' ' + (parsed.from || translate('unknown')), {
                    body: parsed.subject || translate('(no subject)'),
                    icon: '/assets/icon.png'
                });
            }
        });
    }

    if (notificationSupported) {
        if (Notification.permission === 'granted') {
            startSSE();
        } else if (Notification.permission !== 'denied') {
            // Fire-and-forget: no UI depends on the resulting rejection, and
            // requestPermission() itself does not reject under normal use
            // (browsers resolve it even when the user dismisses the prompt).
            void Notification.requestPermission().then(function (p) {
                if (p === 'granted') startSSE();
            });
        }
    } else {
        startSSE();
    }
})();
