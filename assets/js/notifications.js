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

(function () {
    /** @type {boolean} Whether the Notification API exists in this browser. */
    var notificationSupported = typeof Notification !== 'undefined';

    function startSSE() {
        if (!window.EventSource) return;
        var es = new EventSource('/events');
        es.addEventListener('message', function (e) {
            /** @type {{from?: string, subject?: string}} */
            var data;
            try {
                data = JSON.parse(e.data);
            } catch (err) {
                return;
            }
            // Guard on notificationSupported, not just permission: referencing
            // `Notification` when the API does not exist throws a
            // ReferenceError, which this branch is reached precisely when the
            // API is absent (see the fallback call to startSSE() below).
            if (notificationSupported && Notification.permission === 'granted') {
                new Notification('New mail from ' + (data.from || 'unknown'), {
                    body: data.subject || '(no subject)',
                    icon: '/assets/icon.png'
                });
            }
        });
    }

    if (notificationSupported) {
        if (Notification.permission === 'granted') {
            startSSE();
        } else if (Notification.permission !== 'denied') {
            Notification.requestPermission().then(function (p) {
                if (p === 'granted') startSSE();
            });
        }
    } else {
        startSSE();
    }
})();
