// Package events defines the realtime invalidation events exchanged between
// the mail pipeline (sync workers, outbox, HTTP mutations) and the browser.
//
// Events are intentionally small and carry no mail bodies. A client only needs
// enough information to decide which cached queries are stale; the actual data
// is always fetched through the normal REST endpoints. That keeps the SSE
// channel an invalidation bus rather than a second data API.
package events

// Kind identifies what changed. The string values are part of the browser
// contract (see frontend/src/lib/use-mail-events.ts) and must stay stable.
type Kind string

const (
	// KindMailNew is emitted when the sync worker discovers at least one new
	// message in INBOX. It is the only kind that triggers desktop/Web Push.
	KindMailNew Kind = "mail.new"
	// KindMailFlags is emitted when flags (read/unread, etc.) changed.
	KindMailFlags Kind = "mail.flags"
	// KindMailDeleted is emitted when messages disappeared from the mirror.
	KindMailDeleted Kind = "mail.deleted"
	// KindMailSync is a generic "the mailbox changed" event for new messages in
	// non-INBOX folders and full re-syncs.
	KindMailSync Kind = "mail.sync"
	// KindMailChanged is emitted after a successful HTTP mail mutation. It
	// covers handlers that write local state without waiting for the IMAP sync
	// worker, and it is what keeps other tabs in step.
	KindMailChanged Kind = "mail.changed"
	// KindConversationUpdated is emitted when background AI work (summary or
	// suggested reply) finishes for a message.
	KindConversationUpdated Kind = "conversation.updated"
	// KindOutboxUpdated is emitted when the outbox worker records a terminal
	// state (sent, failed or unknown) for an outgoing message.
	KindOutboxUpdated Kind = "outbox.updated"
	// KindResync tells the client to refetch everything. It is sent once when
	// an SSE connection opens, so a reconnect cannot leave stale caches.
	KindResync Kind = "resync"
)

// Event is one invalidation message. UserID and Username are routing metadata
// and are never serialised to the browser.
type Event struct {
	UserID   string `json:"-"`
	Username string `json:"-"`
	Kind     Kind   `json:"type"`
	// AccountID scopes the change to one mailbox account. It is advisory; the
	// current UI invalidates all mail queries for the owner.
	AccountID string `json:"accountId,omitempty"`
	// ConversationID is set when the producer knows the affected conversation.
	ConversationID string `json:"conversationId,omitempty"`
	// Folder is the IMAP folder that changed, when known.
	Folder string `json:"folder,omitempty"`
	// MessageID is the message UID (or outbox ID) that changed, when known.
	MessageID string `json:"messageId,omitempty"`
	// From/Subject are only populated for KindMailNew so the browser can show
	// a notification without fetching the message first.
	From    string `json:"from,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Origin identifies the browser tab that caused an HTTP mutation. The
	// originating tab already applied the change locally and ignores its own
	// event, avoiding a duplicate full refetch; other tabs still process it.
	Origin string `json:"origin,omitempty"`
}

// Publisher is implemented by the realtime hub. A nil publisher (tests, or a
// mailstore used outside the HTTP server) is a no-op.
type Publisher interface {
	Publish(Event)
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(Event)

func (f PublisherFunc) Publish(ev Event) {
	if f != nil {
		f(ev)
	}
}
