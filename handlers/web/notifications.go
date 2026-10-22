// handlers/web/notifications.go
//
// Realtime mail invalidation over Server-Sent Events.
//
// Architecture
// ============
//   - NotificationHub is a process-local fan-out hub. Mail writers (the IMAP
//     sync worker, the outbox worker, background AI jobs and successful HTTP
//     mutations) publish events.Events; the hub routes them to every open tab
//     for that user.
//   - GET /events is the only transport. It is always registered: the React
//     client no longer polls mail endpoints, so SSE is core infrastructure, not
//     an optional notification feature. The [notifications] switches only
//     control desktop toasts and Web Push.
//   - A per-user IMAP IDLE watcher is an accelerator, not a second event
//     source. When it sees a new INBOX message it calls Syncer.Trigger; the
//     normal sync worker then writes the mirror and emits exactly one event
//     after the commit. That ordering is what makes "event => refetch" safe.
//   - Every connection starts with a resync event. Missed events during a
//     disconnect therefore cannot leave a client stale; a replay log is not
//     needed for an invalidation-only channel.
//
// Multi-instance note: the hub is deliberately in-process. The mail mirror is
// a local SQLite file, so a deployment that shares one mailbox mirror across
// replicas is not supported today. If that changes, route Publish through a
// shared bus (Postgres LISTEN/NOTIFY or Redis) before scaling out.
package web

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"

	"inbrix/config"
	"inbrix/events"
	"inbrix/handlers/api"
	"inbrix/models"
)

const (
	sseHeartbeatInterval = 15 * time.Second
	// Closing a stream periodically forces EventSource to reconnect, which
	// re-runs session middleware (so an expired 24h session cannot keep
	// receiving mail forever) and triggers a fresh resync.
	sseMaxConnectionAge = 30 * time.Minute
	sseEventBuffer      = 32

	defaultMaxConnectionsPerUser = 8
	defaultMaxIdleWatchers       = 64
)

// ErrTooManyConnections is returned when a user already has the configured
// number of SSE tabs open.
var ErrTooManyConnections = errors.New("too many realtime connections")

// MailEvent is kept as an alias for the push code and its tests, which used
// this name before the event type moved to the shared events package.
type MailEvent = events.Event

type subscriber struct {
	ch           chan events.Event
	overflow     chan struct{}
	overflowOnce sync.Once
}

// userState holds the per-user subscribers and the optional IDLE accelerator.
type userState struct {
	subs     map[*subscriber]struct{}
	username string
	stop     chan struct{} // non-nil while a watcher goroutine is running
}

// NotificationHub manages SSE subscriptions and per-user IMAP IDLE triggers.
type NotificationHub struct {
	mu           sync.Mutex
	users        map[string]*userState // keyed by user ID
	revisions    map[string]uint64     // per-user change counter for resync fast path
	revisionBase uint64                // restart epoch; keeps stale client revisions from matching
	idleWatchers int
	config       *config.Config
	store        *session.Store
	auth         *AuthHandler
	metrics      realtimeMetrics
	// Optional Web Push fields — nil when webpush is disabled.
	vapidKeys *VAPIDKeys
	pushStore *PushStore

	triggerMu sync.RWMutex
	trigger   func(accountID string)
}

// NewNotificationHub creates a hub ready to accept SSE subscribers.
// auth may be nil in tests; store and cfg are used by HandleSSE and the IDLE
// accelerator. vapidKeys and pushStore may be nil when Web Push is disabled.
func NewNotificationHub(store *session.Store, cfg *config.Config, auth *AuthHandler, vapidKeys *VAPIDKeys, pushStore *PushStore) *NotificationHub {
	return &NotificationHub{
		users:        make(map[string]*userState),
		revisions:    make(map[string]uint64),
		revisionBase: uint64(time.Now().UnixMilli()),
		config:       cfg,
		store:        store,
		auth:         auth,
		vapidKeys:    vapidKeys,
		pushStore:    pushStore,
	}
}

func (h *NotificationHub) maxConnectionsPerUser() int {
	if h != nil && h.config != nil && h.config.Notifications.MaxConnectionsPerUser > 0 {
		return h.config.Notifications.MaxConnectionsPerUser
	}
	return defaultMaxConnectionsPerUser
}

func (h *NotificationHub) maxIdleWatchers() int {
	if h != nil && h.config != nil && h.config.Notifications.MaxIdleWatchers > 0 {
		return h.config.Notifications.MaxIdleWatchers
	}
	return defaultMaxIdleWatchers
}

// Revision returns the per-user change counter. /api/conversations uses it to
// answer a reconnect resync with "unchanged" instead of rebuilding the entire
// conversation list when nothing happened while the tab was offline.
func (h *NotificationHub) ensureRevisionLocked(userID string) uint64 {
	if h.revisionBase == 0 {
		h.revisionBase = uint64(time.Now().UnixMilli())
	}
	rev := h.revisions[userID]
	if rev == 0 {
		rev = h.revisionBase
		h.revisions[userID] = rev
	}
	return rev
}

func (h *NotificationHub) Revision(userID string) uint64 {
	if h == nil || userID == "" {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// The base is a startup timestamp (not 1), so a client that reconnects
	// after a server restart cannot accidentally match its old revision.
	return h.ensureRevisionLocked(userID)
}

// BumpRevision advances the user's change counter without sending an event.
// HTTP mutation middleware calls it before the handler runs, so a read that
// races the response never sees the old revision with new data.
func (h *NotificationHub) BumpRevision(userID string) {
	if h == nil || userID == "" {
		return
	}
	h.mu.Lock()
	h.ensureRevisionLocked(userID)
	h.revisions[userID]++
	h.mu.Unlock()
}

// MetricsSnapshot returns aggregate realtime counters for monitoring.
func (h *NotificationHub) MetricsSnapshot() RealtimeMetricsSnapshot {
	if h == nil {
		return RealtimeMetricsSnapshot{}
	}
	return h.metrics.snapshot()
}

// SetTrigger wires the accelerator to the sync manager. The callback must be
// non-blocking (SyncManager.Trigger is).
func (h *NotificationHub) SetTrigger(trigger func(accountID string)) {
	if h == nil {
		return
	}
	h.triggerMu.Lock()
	h.trigger = trigger
	h.triggerMu.Unlock()
}

func (h *NotificationHub) triggerSync(accountID string) {
	if h == nil || accountID == "" {
		return
	}
	h.triggerMu.RLock()
	trigger := h.trigger
	h.triggerMu.RUnlock()
	if trigger != nil {
		trigger(accountID)
	}
}

// Subscribe registers a tab as a receiver of mail events for userID. It
// returns a channel that is closed if the tab falls behind; the handler then
// closes the HTTP stream so EventSource reconnects and performs a resync.
//
// If this is the first subscriber and the IDLE accelerator is enabled, an IMAP
// watcher is started (subject to the global watcher cap). The IMAP credentials
// are snapshotted synchronously because the Fiber context is only valid for the
// request; the network dial and blocking IDLE loop run in the watcher goroutine.
func (h *NotificationHub) Subscribe(userID, username, accountID string, ch chan events.Event, c *fiber.Ctx) (<-chan struct{}, error) {
	if h == nil || userID == "" || ch == nil {
		return nil, errors.New("invalid realtime subscription")
	}
	sub := &subscriber{ch: ch, overflow: make(chan struct{})}

	h.mu.Lock()
	st := h.users[userID]
	if st == nil {
		st = &userState{subs: make(map[*subscriber]struct{})}
		h.users[userID] = st
	}
	if len(st.subs) >= h.maxConnectionsPerUser() {
		h.mu.Unlock()
		h.metrics.connectionsRejected.Add(1)
		return nil, ErrTooManyConnections
	}
	st.subs[sub] = struct{}{}
	st.username = username
	h.metrics.connections.Add(1)
	h.metrics.connects.Add(1)
	h.mu.Unlock()

	h.triggerMu.RLock()
	hasTrigger := h.trigger != nil
	h.triggerMu.RUnlock()

	h.mu.Lock()
	needWatcher := st.stop == nil &&
		c != nil &&
		h.config != nil &&
		h.config.Notifications.Idle &&
		accountID != "" &&
		h.auth != nil &&
		hasTrigger
	if needWatcher && h.idleWatchers >= h.maxIdleWatchers() {
		needWatcher = false
		h.metrics.idleWatcherSkipped.Add(1)
	}
	h.mu.Unlock()

	if !needWatcher {
		return sub.overflow, nil
	}

	clientFactory, err := h.auth.MailClientFactory(c)
	if err != nil {
		log.Printf("notifications: watcher for user %s: snapshot IMAP credentials: %v", userID, err)
		return sub.overflow, nil
	}

	h.mu.Lock()
	st = h.users[userID]
	if st == nil || st.stop != nil || h.idleWatchers >= h.maxIdleWatchers() {
		h.mu.Unlock()
		if st != nil && st.stop == nil {
			h.metrics.idleWatcherSkipped.Add(1)
		}
		return sub.overflow, nil
	}
	stop := make(chan struct{})
	st.stop = stop
	h.idleWatchers++
	h.metrics.idleWatchers.Add(1)
	h.mu.Unlock()

	go h.runWatcher(userID, accountID, stop, clientFactory)
	return sub.overflow, nil
}

// Unsubscribe removes ch from the subscriber list for userID. When the last
// subscriber leaves, the watcher goroutine is signalled to stop.
func (h *NotificationHub) Unsubscribe(userID string, ch chan events.Event) {
	if h == nil || userID == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	st := h.users[userID]
	if st == nil {
		return
	}
	var removed *subscriber
	for sub := range st.subs {
		if sub.ch == ch {
			removed = sub
			delete(st.subs, sub)
			break
		}
	}
	if removed == nil {
		return
	}
	h.metrics.connections.Add(-1)
	h.metrics.disconnects.Add(1)
	if len(st.subs) > 0 {
		return
	}
	if st.stop != nil {
		close(st.stop)
		st.stop = nil
	}
	delete(h.users, userID)
}

// DisconnectUser closes every SSE stream for a user. It is called on logout so
// a deleted session cannot keep receiving mail events for up to the 30-minute
// connection lifetime.
func (h *NotificationHub) DisconnectUser(userID string) {
	if h == nil || userID == "" {
		return
	}
	h.mu.Lock()
	st := h.users[userID]
	if st != nil {
		for sub := range st.subs {
			sub.overflowOnce.Do(func() { close(sub.overflow) })
		}
		if st.stop != nil {
			close(st.stop)
			st.stop = nil
		}
	}
	h.mu.Unlock()
}

// Publish fans an event out to every tab for its user. A slow tab is
// disconnected rather than silently missing the event: the handler closes the
// stream, EventSource reconnects, and the resync frame repairs its caches.
// Desktop/Web Push are sent even when no tab is connected.
func (h *NotificationHub) Publish(ev events.Event) {
	if h == nil || ev.UserID == "" {
		return
	}

	h.mu.Lock()
	st := h.users[ev.UserID]
	subs := make([]*subscriber, 0)
	if st != nil {
		subs = make([]*subscriber, 0, len(st.subs))
		for sub := range st.subs {
			subs = append(subs, sub)
		}
	}
	username := ev.Username
	if username == "" && st != nil {
		username = st.username
	}
	h.ensureRevisionLocked(ev.UserID)
	h.revisions[ev.UserID]++
	ev.Revision = h.revisions[ev.UserID]
	h.mu.Unlock()

	h.metrics.eventsPublished.Add(1)
	for _, sub := range subs {
		select {
		case sub.ch <- ev:
		default:
			h.metrics.eventsDropped.Add(1)
			sub.overflowOnce.Do(func() {
				h.metrics.overflows.Add(1)
				close(sub.overflow)
				log.Printf("sse: subscriber overflow user=%s; forcing reconnect for resync", ev.UserID)
			})
		}
	}

	if ev.Kind != events.KindMailNew || h.config == nil || !h.config.Notifications.Enabled {
		return
	}
	if h.config.Notifications.Desktop {
		notifyDesktop("New mail from "+ev.From, ev.Subject)
	}
	if h.config.Notifications.WebPush && h.vapidKeys != nil && h.pushStore != nil && username != "" {
		go SendPush(username, ev, h.vapidKeys, h.pushStore)
	}
}

// runWatcher watches INBOX for the account and nudges the sync worker. It does
// not broadcast directly: the sync worker emits the event after committing the
// new message to SQLite, so clients never refetch before the data exists.
func (h *NotificationHub) runWatcher(userID, accountID string, stop chan struct{}, open func() (api.MailClient, error)) {
	defer func() {
		h.mu.Lock()
		h.idleWatchers--
		h.metrics.idleWatchers.Add(-1)
		if st := h.users[userID]; st != nil && st.stop == stop {
			st.stop = nil
		}
		h.mu.Unlock()
	}()

	select {
	case <-stop:
		return
	default:
	}

	client, err := open()
	if err != nil {
		h.metrics.idleWatcherFailures.Add(1)
		log.Printf("notifications: watcher for user %s: create IMAP client: %v", userID, err)
		return
	}
	if client == nil {
		h.metrics.idleWatcherFailures.Add(1)
		return
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("notifications: watcher for user %s: close: %v", userID, err)
		}
	}()

	select {
	case <-stop:
		return
	default:
	}

	err = client.WatchInbox(stop, func(_ models.Email) {
		h.triggerSync(accountID)
	})
	if err != nil {
		h.metrics.idleWatcherFailures.Add(1)
		log.Printf("notifications: watcher for user %s stopped: %v", userID, err)
	}
}

// NotificationsHandler wraps the hub and implements the SSE HTTP handler.
type NotificationsHandler struct {
	hub *NotificationHub
}

// NewNotificationsHandler wires up the hub.
func NewNotificationsHandler(hub *NotificationHub) *NotificationsHandler {
	return &NotificationsHandler{hub: hub}
}

// HandleSSE is the Fiber handler for GET /events. It streams Server-Sent
// Events for as long as the connection stays open. Every connection begins
// with a resync event and a retry hint; a 15s comment heartbeat keeps proxies
// from treating the connection as idle.
func (nh *NotificationsHandler) HandleSSE(c *fiber.Ctx) error {
	if nh == nil || nh.hub == nil {
		return fiber.ErrServiceUnavailable
	}
	userID, _ := c.Locals("user_id").(string)
	username, _ := c.Locals("username").(string)
	if userID == "" {
		return fiber.ErrUnauthorized
	}
	accountID := ""
	if nh.hub.store != nil {
		if sess, err := requestSession(c, nh.hub.store); err == nil {
			accountID, _ = sess.Get("account_id").(string)
		}
	}

	ch := make(chan events.Event, sseEventBuffer)
	overflow, err := nh.hub.Subscribe(userID, username, accountID, ch, c)
	if err != nil {
		c.Set("Retry-After", "5")
		return fiber.NewError(fiber.StatusTooManyRequests, "too many realtime connections")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no") // Tell nginx not to buffer SSE.

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer nh.hub.Unsubscribe(userID, ch)

		// Reconnect quickly, then force a full resync: events missed while the
		// connection was down are covered by refetching current state.
		if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
			return
		}
		resync, err := json.Marshal(events.Event{
			Kind:     events.KindResync,
			Revision: nh.hub.Revision(userID),
		})
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", resync); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}

		heartbeat := time.NewTicker(sseHeartbeatInterval)
		defer heartbeat.Stop()
		maxAge := time.NewTimer(sseMaxConnectionAge)
		defer maxAge.Stop()

		for {
			select {
			case <-overflow:
				// The tab fell behind. Closing the stream is the recovery
				// mechanism: EventSource reconnects and resyncs.
				return
			case ev := <-ch:
				payload, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-maxAge.C:
				return
			}
		}
	})

	return nil
}
