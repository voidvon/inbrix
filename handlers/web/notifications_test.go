package web

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"inbrix/config"
	"inbrix/events"
)

func testNotificationHub() *NotificationHub {
	cfg := &config.Config{}
	cfg.Notifications.Idle = false
	return NewNotificationHub(nil, cfg, nil, nil, nil)
}

func subscribeTest(t *testing.T, hub *NotificationHub, userID string, ch chan events.Event) <-chan struct{} {
	t.Helper()
	overflow, err := hub.Subscribe(userID, "alice", "", ch, nil)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return overflow
}

func receiveEvent(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for event")
		return events.Event{}
	}
}

func expectNoEvent(t *testing.T, ch <-chan events.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotificationHubFansOutPerUserAndUnsubscribes(t *testing.T) {
	hub := testNotificationHub()
	alice1 := make(chan events.Event, 4)
	alice2 := make(chan events.Event, 4)
	bob := make(chan events.Event, 4)
	subscribeTest(t, hub, "user-alice", alice1)
	subscribeTest(t, hub, "user-alice", alice2)
	subscribeTest(t, hub, "user-bob", bob)
	defer hub.Unsubscribe("user-alice", alice1)
	defer hub.Unsubscribe("user-alice", alice2)
	defer hub.Unsubscribe("user-bob", bob)

	hub.Publish(events.Event{UserID: "user-alice", Kind: events.KindMailChanged})
	if ev := receiveEvent(t, alice1); ev.Kind != events.KindMailChanged {
		t.Fatalf("alice1 kind = %q", ev.Kind)
	}
	if ev := receiveEvent(t, alice2); ev.Kind != events.KindMailChanged {
		t.Fatalf("alice2 kind = %q", ev.Kind)
	}
	expectNoEvent(t, bob)

	hub.Unsubscribe("user-alice", alice1)
	hub.Unsubscribe("user-alice", alice2)
	hub.Publish(events.Event{UserID: "user-alice", Kind: events.KindMailNew})
	expectNoEvent(t, alice1)
	expectNoEvent(t, alice2)
}

func TestMailMutationEventsOnlyPublishesForMailMutations(t *testing.T) {
	hub := testNotificationHub()
	ch := make(chan events.Event, 8)
	subscribeTest(t, hub, "user-alice", ch)
	defer hub.Unsubscribe("user-alice", ch)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "user-alice")
		c.Locals("username", "alice")
		return c.Next()
	})
	app.Use(MailMutationEvents(hub))
	app.Post("/api/conversations/:id/read", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Put("/api/conversations/:id/ai-chat", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Post("/api/settings/example", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/api/conversations", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest(fiber.MethodPost, "/api/conversations/c1/read", nil)
	req.Header.Set("X-Client-Id", "tab-test")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("mail mutation: status=%v err=%v", resp, err)
	}
	if ev := receiveEvent(t, ch); ev.Kind != events.KindMailChanged || ev.Origin != "tab-test" {
		t.Fatalf("mutation event = %+v", ev)
	}
	if rev := hub.Revision("user-alice"); rev == 0 {
		t.Fatal("mail mutation did not advance the revision")
	}

	// Autosave and non-mail mutations must stay silent.
	before := hub.Revision("user-alice")
	if _, err := app.Test(httptest.NewRequest(fiber.MethodPut, "/api/conversations/c1/ai-chat", nil)); err != nil {
		t.Fatal(err)
	}
	expectNoEvent(t, ch)
	if got := hub.Revision("user-alice"); got != before {
		t.Fatalf("ai-chat autosave advanced revision: %d -> %d", before, got)
	}
	if _, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/api/settings/example", nil)); err != nil {
		t.Fatal(err)
	}
	expectNoEvent(t, ch)
	if _, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/conversations", nil)); err != nil {
		t.Fatal(err)
	}
	expectNoEvent(t, ch)
}

func TestNotificationHubOverflowForcesReconnect(t *testing.T) {
	hub := testNotificationHub()
	ch := make(chan events.Event, 1)
	overflow := subscribeTest(t, hub, "user-alice", ch)
	defer hub.Unsubscribe("user-alice", ch)

	hub.Publish(events.Event{UserID: "user-alice", Kind: events.KindMailChanged})
	hub.Publish(events.Event{UserID: "user-alice", Kind: events.KindMailChanged})

	select {
	case <-overflow:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("overflow channel was not closed")
	}
	metrics := hub.MetricsSnapshot()
	if metrics.EventsDropped == 0 || metrics.Overflows == 0 {
		t.Fatalf("overflow metrics = %+v", metrics)
	}
}

func TestNotificationHubConnectionLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Notifications.Idle = false
	cfg.Notifications.MaxConnectionsPerUser = 1
	hub := NewNotificationHub(nil, cfg, nil, nil, nil)

	first := make(chan events.Event, 1)
	subscribeTest(t, hub, "user-alice", first)
	defer hub.Unsubscribe("user-alice", first)

	second := make(chan events.Event, 1)
	if _, err := hub.Subscribe("user-alice", "alice", "", second, nil); !errors.Is(err, ErrTooManyConnections) {
		t.Fatalf("second Subscribe error = %v, want ErrTooManyConnections", err)
	}
	if metrics := hub.MetricsSnapshot(); metrics.ConnectionsRejected != 1 {
		t.Fatalf("ConnectionsRejected = %d", metrics.ConnectionsRejected)
	}
}

func TestNotificationHubDisconnectUserClosesStreams(t *testing.T) {
	hub := testNotificationHub()
	ch := make(chan events.Event, 4)
	overflow := subscribeTest(t, hub, "user-alice", ch)
	defer hub.Unsubscribe("user-alice", ch)

	hub.DisconnectUser("user-alice")
	select {
	case <-overflow:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("DisconnectUser did not close the stream")
	}
}

func TestNotificationHubRevisionIncrements(t *testing.T) {
	hub := testNotificationHub()
	ch := make(chan events.Event, 4)
	subscribeTest(t, hub, "user-alice", ch)
	defer hub.Unsubscribe("user-alice", ch)

	base := hub.Revision("user-alice")
	if base == 0 {
		t.Fatal("initial revision is zero")
	}
	hub.Publish(events.Event{UserID: "user-alice", Kind: events.KindMailChanged})
	if rev := hub.Revision("user-alice"); rev != base+1 {
		t.Fatalf("revision after publish = %d, want %d", rev, base+1)
	}
	hub.BumpRevision("user-alice")
	if rev := hub.Revision("user-alice"); rev != base+2 {
		t.Fatalf("revision after bump = %d, want %d", rev, base+2)
	}
}
