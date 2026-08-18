# lilmail `/v1` API — the PIM contract

`/v1` is lilmail's stable, machine-readable **contract**: JSON mail + calendar +
contacts served over one HTTP surface. lilmail is a standalone PIM client — it
connects to the **user's own** IMAP/SMTP/CalDAV/CardDAV account and exposes what
it reads/writes there as `/v1`. It hosts no mail server and depends on no central
server. When the local SQLite mirror is enabled, one application account may own
several mailboxes; the active mailbox selected in the session scopes each `/v1`
mail request.

`/v1` is the shared source of truth other UIs build on:

- the **Vulos OS** thin Calendar/Contacts widgets and mail surface consume it;
- lilmail's own React browser client is a first-class consumer too;
- any third-party tool or script can drive it.

The API returns `models.Email` / `MailboxInfo` / `models.Calendar*` /
`models.Contact` JSON and never renders HTML, so it is a stable seam an
external UI can build against. It is the backend used by the React SPA and shares the
same engine + authentication — using `/v1` never changes the standalone UI.

> **Stability.** `/v1` is load-bearing: external UIs build on it. The mail,
> `/v1/calendar/*`, and `/v1/contacts` shapes are treated as a stable contract —
> changes are additive.

## Authentication

lilmail can be used without an application account through direct mailbox login.
With the local SQLite mirror enabled, it also provides a local application
account (`/register` and `/user-login`) whose password is separate from mailbox
passwords. A user can attach several own mailboxes and switch the active one;
every `/v1` route remains owner-scoped by the server-side session and there is
no account/tenant/user path segment or caller-controlled mailbox selector. The
*only* remote credentials are still the user's own IMAP/SMTP password or an
OAuth2 access token for their provider.

There are exactly **two** ways a request can be authenticated:

| Mode | How the caller proves itself | Who uses it |
|------|------------------------------|-------------|
| **Session cookie** (default) | The cookie set by `POST /login`, `POST /user-login`, or the OAuth2 callback | browsers, `curl -b cookies.txt`, lilmail's React client |
| **Injected credentials** (opt-in, off by default) | `X-Vulos-Broker-Auth` + `X-Vulos-Mail-*` headers | an embedding host that already holds the user's mailbox credentials |

Both resolve to the same thing — one mailbox connection for the duration of one
request. There is no API-key or bearer-token scheme, no refresh endpoint of
lilmail's own, and no way to mint a long-lived credential from `/v1`.

The API reuses lilmail's session cookie — the **same** session established by
`POST /login`, `POST /user-login`, or the OAuth2 flow. There is no separate API
token scheme. The local SQLite mirror is used by the HTML inbox path; `/v1`
continues to expose the live mail engine contract and is not a second database
API.

When the session is missing or unauthenticated, the API responds **`401` with a
JSON body**, so a `fetch()`-based client can react in code:

```json
{ "error": "not authenticated" }
```

Send requests with credentials included (e.g. `fetch(url, { credentials: 'include' })`).

### Injected-credential mode (embedding hosts)

Normally lilmail holds its own session and connects to the user's mailbox itself.
As an **option**, an embedding host (or the test harness) may inject the
per-request connection descriptor as HTTP headers, so lilmail builds the IMAP/
SMTP/DAV client **directly from the headers** instead of from a session. These
headers only ever describe the **user's own** account (endpoint + a short-lived
OAuth token or password) that lilmail then talks to — lilmail hosts no mail and
depends on no central server.

This path is **off by default** and gated by a shared secret:

- Set `LILMAIL_BROKER_SECRET` (environment variable) on the lilmail process.
- Every request must send `X-Vulos-Broker-Auth: <secret>`. lilmail compares it
  against `LILMAIL_BROKER_SECRET` in **constant time**.
- **If `LILMAIL_BROKER_SECRET` is unset, or the presented secret does not match,
  the `X-Vulos-Mail-*` headers are ignored entirely** and the request falls back
  to normal session auth. Standalone lilmail therefore never trusts arbitrary
  client-supplied connection headers.

When the secret validates, lilmail reads the connection spec from these headers:

| Header | Meaning |
|--------|---------|
| `X-Vulos-Mail-Provider`  | `gmail` \| `outlook` \| `imap` (informational) |
| `X-Vulos-Mail-Email`     | mailbox address (used as MAIL FROM / identity) |
| `X-Vulos-Mail-Username`  | IMAP/SMTP login username (defaults to the email) |
| `X-Vulos-Mail-Auth`      | `xoauth2` (access token) \| `plain` (password) |
| `X-Vulos-Mail-Secret`    | the XOAUTH2 access token, or the IMAP/SMTP password |
| `X-Vulos-Mail-Imap-Host` | IMAP host (required) |
| `X-Vulos-Mail-Imap-Port` | IMAP port (default `993`, implicit TLS) |
| `X-Vulos-Mail-Smtp-Host` | SMTP host (defaults to the IMAP host) |
| `X-Vulos-Mail-Smtp-Port` | SMTP port (default `587`; `465` ⇒ implicit TLS, else STARTTLS) |
| `X-Vulos-Mail-Caldav-Url`  | CalDAV base URL for the account (optional; enables `/v1/calendar/*`) |
| `X-Vulos-Mail-Carddav-Url` | CardDAV base URL for the account (optional; enables `/v1/contacts`) |

`xoauth2` builds the IMAP client via `NewClientOAuth(host, port, username, token,
"xoauth2")` and the SMTP client via `NewSMTPClientOAuth`; `plain` uses
`NewClient(host, port, username, password)` / `NewSMTPClient`. This path covers
the mail routes — folders, messages, single message, search, flags, delete, move,
compose (`POST /v1/messages`) and drafts (`POST /v1/drafts`).

**Calendar & contacts.** When `X-Vulos-Mail-Caldav-Url` /
`X-Vulos-Mail-Carddav-Url` are present, the `/v1/calendar/*` and `/v1/contacts`
routes are served from those per-account DAV base URLs instead of the session.
Authentication reuses `X-Vulos-Mail-Auth: xoauth2` + `X-Vulos-Mail-Secret`: the
access token is presented to CalDAV/CardDAV as an HTTP `Authorization: Bearer
<token>` header. If the relevant DAV URL header is **absent**, the read routes
return an empty result (`{ "events": [] }` / `{ "busy": [] }` /
`{ "contacts": [] }`) and the write routes return `501 Not Implemented` — the
session is never touched. These routes are registered whenever CalDAV/CardDAV is
enabled in config **or** the injected-credential path is active
(`LILMAIL_BROKER_SECRET` set).

Note: Outlook/Microsoft 365 calendar & contacts (Microsoft Graph) are **not**
covered by this CalDAV/CardDAV path; only accounts that expose CalDAV/CardDAV
(e.g. Gmail, generic DAV) work here.

The headers are only ever read **inside** the `/v1` group, after the secret has
been validated — never on unauthenticated paths.

**The exact wire format** — header-by-header semantics, the acceptance algorithm,
and an explicit list of the properties this scheme does *not* have (no HMAC, no
timestamp window, no nonce, no replay protection) — is specified in
[SIGNING.md § 1](SIGNING.md#1-mail-broker-seam-inbound). Read it before deploying
the seam: it is only safe over a trusted transport.

> **No webhooks.** lilmail never calls out to a URL you supply. There is no
> webhook registration surface, no event delivery, and no outbound signature
> scheme — see [SIGNING.md § 0](SIGNING.md#0-lilmail-emits-no-webhooks) for what
> to use instead (SSE, Web Push, or polling).

## Conventions

- **Folders** travel as the `folder` query parameter (default `INBOX`). This
  avoids escaping the IMAP hierarchy delimiter — `?folder=INBOX/Archive` works
  verbatim.
- **UIDs** are numeric and appear as path segments.
- **Pagination** on `/v1/messages`: `limit` caps the page size and `offset` skips
  the newest N messages, so page *k* = `?limit=L&offset=k*L`. The response echoes
  the effective `limit`/`offset` and sets `nextOffset` to the next page's offset
  (`null` when the returned page was short, i.e. the end of the mailbox).
- Errors are always `{ "error": "<message>" }` with an appropriate status code
  (`400` bad request, `401` unauthenticated, `404` not found, `409` conflict,
  `429` rate-limited, `501` not implemented, `502` upstream mail-server failure).
- All payloads follow the `models.Email` / `MailboxInfo` shapes (see
  `models/email.go`, `handlers/api/client.go`).

## Endpoints

| Method | Path | Query | Body | Returns |
|--------|------|-------|------|---------|
| `GET`    | `/v1/me`                       | —                       | —              | `{ email, username }` |
| `GET`    | `/v1/folders`                  | —                       | —              | `{ folders: MailboxInfo[] }` |
| `POST`   | `/v1/folders`                  | —                       | `{ name }`     | `201 { folder }` |
| `DELETE` | `/v1/folders`                  | `folder`                | `{ name }` (or `?folder=`) | `204` |
| `GET`    | `/v1/messages`                 | `folder`, `limit` (50), `offset` (0) | —  | `{ folder, limit, offset, nextOffset, messages: Email[] }` |
| `GET`    | `/v1/messages/:uid`            | `folder`                | —              | `Email` (incl. `attachments[]`) |
| `GET`    | `/v1/messages/:uid/attachments/:partId` | `folder`       | —              | attachment bytes (streamed) |
| `GET`    | `/v1/search`                   | `folder`, `q`, `limit` (100) | —         | `{ folder, query, messages: Email[] }` |
| `PATCH`  | `/v1/messages/:uid/flags`      | `folder`                | `{ flag, add }` or `{ flags[], add }` | `204` |
| `DELETE` | `/v1/messages/:uid`            | `folder`, `hard`        | —              | `204` |
| `POST`   | `/v1/messages/:uid/move`       | `folder`                | `{ toFolder, folder? }` | `204` |
| `POST`   | `/v1/messages/:uid/spam`       | `folder`                | —              | `{ folder }` (Junk/Spam target) |
| `POST`   | `/v1/messages/:uid/snooze`     | `folder`                | `{ until }`    | `200 { snoozed, autoReturn:false, until, folder, note }` |
| `DELETE` | `/v1/messages/:uid/snooze`     | `folder`                | —              | `204` |
| `POST`   | `/v1/messages`                 | —                       | `{ to, cc?, bcc?, subject, text?, html?, inReplyTo?, attachments?, sendAt? }`¹ | `201 { sent: true }`, or `202 { scheduled, id, sendAt }` when `sendAt` is set |
| `POST`   | `/v1/drafts`                   | —                       | `{ to?, cc?, subject?, text?, html?, inReplyTo?, attachments? }`¹     | `201 { saved: true }` |
| `POST`   | `/v1/attachments`              | —                       | multipart form, file field `file` | `201 { token, filename, size, contentType }` |
| `GET`    | `/v1/scheduled`                | —                       | —              | `{ scheduled: […] }` (pending send-later) |
| `DELETE` | `/v1/scheduled/:id`            | —                       | —              | `204` |
| `PATCH`  | `/v1/scheduled/:id`            | —                       | `{ sendAt?, subject?, text?, html?, to?, cc?, bcc? }` | updated record |

¹ Each entry of `attachments[]` is `{ token? , data? , filename?, contentType?, contentId?, inline? }`
— see [Attachments](#attachments) for the two-step upload flow and `cid:` inline images.

`POST /v1/folders` creates an IMAP mailbox (a "label" in the mail UI); the name
may not contain the IMAP control characters `\r \n \t * % "` and may not collide
with a protected system folder (Inbox/Sent/Drafts/Spam/Trash/Archive/Snoozed/…),
which return `409`. `DELETE /v1/folders` deletes a user mailbox by `name` (body)
or `?folder=`; system folders are `403`.

`POST /v1/messages/:uid/spam` reports spam by moving the message to the
discovered Junk/Spam folder — there is no separate training-signal endpoint on
this backend, so the move IS the report (pair it with an undo toast like archive).

`POST /v1/messages/:uid/snooze` moves the message to the Snoozed folder and
validates + echoes `until`. lilmail is a client and does not itself run a
delivery-side scheduler, so it does **not** auto-return the message to the inbox:
the response is `200 { snoozed:true, autoReturn:false, until, folder, note }` and
the client is responsible for surfacing the due time / returning the message.
`DELETE /v1/messages/:uid/snooze` is a no-op acknowledgement kept for symmetry
(the caller moves the message back itself); it returns `204`.

`DELETE /v1/messages/:uid` MOVES the message to the Trash folder by default
(discovered via the `\Trash` special-use, with name fallbacks Trash / Deleted /
Deleted Items / Bin). Pass `?hard=true` (or `?hard=1`) to permanently expunge
instead. If the source folder already IS the Trash folder, or no Trash folder
can be located, the delete falls back to a permanent expunge.

`POST /v1/messages/:uid/move` moves a message between folders (e.g. archive).
The source folder comes from the `folder` query param (default `INBOX`); an
optional non-empty `folder` field in the body overrides it. `toFolder` is
required.

`POST /v1/messages` is **rate-limited per client IP** to prevent spam/relay abuse
(default 30 sends / 60 s, configurable via `[rate_limit]` in `config.toml`); the
cap returns `429 { "error": "rate limit exceeded" }`.

### Attachments

**Download.** `GET /v1/messages/:uid/attachments/:partId?folder=` streams a
single MIME part on demand (the content is NOT included in the message listing).
`partId` is the IMAP MIME part path — take it from an entry's `partId` in the
message's `attachments[]` array (see the `Email` shape below). The response
carries the part's `Content-Type` and a `Content-Disposition: attachment;
filename="…"` (with an RFC 5987 `filename*` form for non-ASCII names). Both the
content type and filename are sanitized against response-header injection; an
untrusted/malformed content type falls back to `application/octet-stream`.
Downloads are capped at 25 MiB. Works in both session and injected-credential modes.
Unknown part / message ⇒ `404`; unauthenticated ⇒ `401`.

**Upload (compose).** Attaching a file to an outgoing message is a two-step,
JSON-friendly flow:

1. `POST /v1/attachments` — multipart form with a `file` field. Stages the bytes
   under the caller's per-account namespace and returns
   `201 { token, filename, size, contentType }`.
2. `POST /v1/messages` (or `/v1/drafts`) — reference the staged upload in the
   `attachments` array: `{"attachments":[{"token":"<token>"}]}`. Each token is
   resolved and CONSUMED (single-use, so it cannot be replayed). `filename` /
   `contentType` may be supplied per-entry to override the staged metadata.

Alternatively, small files can be sent fully inline (no step 1) with
`{"filename":"a.txt","contentType":"text/plain","data":"<base64>"}`. A single
attachment (and the per-message total) is capped at 25 MiB. Staged uploads that
are never sent are garbage-collected after 24 h.

**Inline images (`cid:`).** To embed an image *inside* the HTML body (rather than
send it as a downloadable file), mark the attachment ref `inline` and give it a
`contentId`, then reference that id from the HTML with `<img src="cid:ID">`:

```json
{
  "to": "bob@example.com",
  "subject": "Hi",
  "html": "<p>See <img src=\"cid:logo1\"></p>",
  "attachments": [
    { "contentId": "logo1", "inline": true,
      "contentType": "image/png", "data": "<base64>" }
  ]
}
```

The two new per-entry fields extend the existing attachment ref shape:

| field       | type    | meaning |
|-------------|---------|---------|
| `contentId` | string  | Bare cid token — **no** `cid:` scheme, **no** angle brackets. Must match the `cid:ID` in the HTML. Required when `inline`. Validated against header injection (`[A-Za-z0-9._%+-]+(@host)?`; CRLF/space/`<>"` rejected). |
| `inline`    | boolean | `true` ⇒ the part is emitted with `Content-Disposition: inline` and a `Content-ID: <contentId>` header inside a `multipart/related` container, so it renders in-body. |

`inline` and the byte source are orthogonal: an inline image can be supplied by
`token` (staged upload) **or** by base64 `data`. An `inline` ref without a
`contentId` is a `400`; an `inline` ref on a message with no `html` body degrades
to a normal attachment (nothing could reference it).

Resulting MIME structure:

- **inline only** → `multipart/related( multipart/alternative(text, html), inline-parts… )`
- **inline + regular attachments** →
  `multipart/mixed( multipart/related( multipart/alternative(text, html), inline-parts… ), attachments… )`
- **no inline parts** → unchanged (`multipart/mixed`/`alternative`/plain as before).

This lets clients avoid shipping fat `data:image/…;base64,…` URIs inside the HTML
body (which inflate every message ~33 %) and reference `cid:` parts instead. The
web composer inserts CID images from its image picker, clipboard, and drag/drop.
New images are proportionally sized so their longest edge is at most 480 pixels;
the editor resize handle can then set a larger proportional `width`/`height`.

### Scheduled send (send-later)

`POST /v1/messages` with a **future** RFC3339 `sendAt` turns an ordinary send into
a scheduled one: the compose payload is persisted and delivered at the due time by
a background drain, and the call returns `202 { scheduled:true, id, sendAt }`
instead of `201 { sent:true }`. Omit `sendAt` (or pass an empty string) for an
immediate send. A past/absurd `sendAt` is `400`; a value beyond one year is `400`.
For accounts authenticated with a short-lived OAuth token (injected Gmail/Outlook creds,
or a session OAuth login) the horizon tightens to ~12 h — beyond that the captured
token would be expired at fire time, so the schedule is refused up front rather
than accepted and silently failed.

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `GET`    | `/v1/scheduled`      | —                                          | `{ scheduled: [{ id, sendAt, to, cc?, bcc?, subject, created }] }` |
| `DELETE` | `/v1/scheduled/:id`  | —                                          | `204` |
| `PATCH`  | `/v1/scheduled/:id`  | `{ sendAt?, subject?, text?, html?, to?, cc?, bcc? }` (all optional) | updated public record |

Scheduled sends are scoped to the authenticated account: another account's `id`
(or a nonexistent one) is `404`, so listing/cancel/edit never leak across
accounts. Delivery is **at-least-once** — the record is deleted only after a
successful SMTP send, so a crash mid-send re-fires it on the next poll (a rare
duplicate) rather than dropping the mail; a persistently failing send is abandoned
after a bounded retry budget. Every fire rebuilds the MIME through the same
`BuildMIMEMessage` engine as an immediate send, so the header-injection guard and
`cid:` inline handling run at actual send time.

Scheduled send is **enabled by wiring a durable KV store** into the API handler
(`NewWithStore`). Where it is not configured, `POST /v1/messages` with `sendAt`
and the whole `/v1/scheduled` surface return `501 Not Implemented` — an
unconfigured build simply has no send-later, rather than silently dropping mail.

### Calendar (only when `[caldav] enabled`)

Times are RFC 3339 strings. The `start`/`end` range defaults to the current
month when omitted. These reuse the same CalDAV client + `models.Calendar*`
types as the React calendar UI.

| Method | Path | Query | Body | Returns |
|--------|------|-------|------|---------|
| `GET`    | `/v1/calendar/events`          | `start`, `end` | —          | `{ events: CalendarEvent[] }` |
| `POST`   | `/v1/calendar/events`          | —              | `{ summary, start, end, description?, location?, allDay?, recurrence? }` | `201 { created: true }` |
| `PUT`    | `/v1/calendar/events/:uid`     | —              | `{ summary, start, end, description?, location?, allDay?, recurrence?, path? }` | `{ updated: true }` |
| `DELETE` | `/v1/calendar/events/:uid`     | —              | —          | `204` |
| `GET`    | `/v1/calendar/freebusy`        | `start`, `end` | —          | `{ busy: { start, end }[] }` |
| `POST`   | `/v1/calendar/rsvp`            | —              | `{ uid, organizer, response, …event }` | `{ ok: true }` |

`recurrence` is a raw iCalendar RRULE (e.g. `FREQ=WEEKLY;COUNT=10`), stored and
returned verbatim. `CalendarEvent` includes `uid`, `path` (CalDAV object path)
and `recurrence`; pass `path` back on `PUT` so an edit targets the exact object.

`POST /v1/calendar/rsvp` is the **iTIP/iMIP** reply to a received invitation: it
sends a `METHOD:REPLY` back to the `organizer` with the chosen `response`
(`ACCEPTED` / `DECLINED` / `TENTATIVE`) and reflects the event into the
responder's own calendar. A received invite arrives on a message as
`Email.Invite` (attendees plus the recipient's own `MyPartStat`), which the client
reads from `GET /v1/messages/:uid`.

### Contacts (only when `[carddav] enabled`)

Everything here reads and writes the **user's own** CardDAV address book. There
is no lilmail-side contact database: a card exists iff it exists on the user's
CardDAV server, so two lilmail instances pointed at one account see one book.

| Method | Path | Query | Body | Returns |
|--------|------|-------|------|---------|
| `GET`    | `/v1/contacts`                 | `q`, `limit` (50)  | —       | `{ contacts: { email, name }[] }` (autocomplete form) |
| `GET`    | `/v1/contacts/cards`           | `q`, `limit` (500), `group` | — | `{ contacts: Contact[] }` (full cards) |
| `GET`    | `/v1/contacts/frequent`        | `limit` (12, max 50) | —     | `{ contacts: { email, name?, count, lastUsed }[] }` |
| `POST`   | `/v1/contacts`                 | —                  | `Contact` (no `uid`/`path`) | `201 { contact }` |
| `PUT`    | `/v1/contacts/:uid`            | —                  | `Contact` (`path?` targets the exact object) | `{ contact }` |
| `DELETE` | `/v1/contacts/:uid`            | `path?`            | —       | `204` |
| `POST`   | `/v1/contacts/:uid/photo`      | —                  | multipart form, file field `file` | `{ contact }` |
| `DELETE` | `/v1/contacts/:uid/photo`      | —                  | —       | `{ contact }` |
| `GET`    | `/v1/contacts/groups`          | —                  | —       | `{ groups: { name, count }[] }` |
| `POST`   | `/v1/contacts/groups`          | —                  | `{ name }` | `201 { group: { name, count: 0 } }` |
| `PATCH`  | `/v1/contacts/groups/:name`    | —                  | `{ name }` | `{ renamed: <cards rewritten>, name }` |
| `DELETE` | `/v1/contacts/groups/:name`    | —                  | —       | `{ removed: <cards rewritten> }` |
| `POST`   | `/v1/contacts/import`          | —                  | multipart form: `file`, `format?`, `mapping?` | `{ imported, skipped }` |
| `GET`    | `/v1/contacts/export`          | `format` (`vcf`\|`csv`) | —  | file download (`text/vcard` / `text/csv`) |

The literal sub-paths (`cards`, `frequent`, `groups`, `import`, `export`) are
registered **before** `/v1/contacts/:uid`, so they are never captured as a UID.

When the account has no usable CardDAV target — standalone with `[carddav]`
unconfigured, or an injected-credential request with no
`X-Vulos-Mail-Carddav-Url` — **reads degrade to an empty result** (`{"contacts":
[]}` / `{"groups": []}`) and **writes return `501 Not Implemented`** with
`{"error":"contacts not available for this account"}`. Nothing falls back to a
different account's book.

#### `Contact` shape

`GET /v1/contacts/cards` returns these verbatim, and `POST`/`PUT` accept the same
shape. Only `uid`, `name` and `emails` are always present; every other field is
omitted when empty.

```jsonc
{
  "uid": "8f14e45f-ea4c-4b1e-9b06-9f2b3c0a1d77", // vCard UID, server-assigned on create
  "name": "Alice Adams",                          // vCard FN
  "structuredName": {                             // vCard N; derived from name when absent
    "prefix": "Dr", "first": "Alice", "middle": "Q", "last": "Adams", "suffix": "PhD"
  },
  "nickname": "Al",
  "fileAs": "Adams, Alice",                       // SORT-AS / X-ABShowAs list-ordering hint
  "org": "Example Ltd",
  "department": "Platform",                       // ORG component 2
  "title": "Staff Engineer",
  "note": "met at FOSDEM",
  "emails": ["alice@example.com"],                // unlabelled projection (always present)
  "phones": ["+44 20 7946 0000"],
  "typedEmails": [{ "value": "alice@example.com", "type": "work" }],
  "typedPhones": [{ "value": "+44 20 7946 0000", "type": "mobile" }],
  "addresses": [{ "type": "home", "poBox": "", "extended": "", "street": "1 High St",
                  "locality": "London", "region": "", "postal": "SW1A 1AA", "country": "GB" }],
  "websites": [{ "value": "https://example.com", "type": "work" }],
  "ims":      [{ "value": "alice@xmpp.example", "type": "xmpp" }],
  "birthday": "1990-04-01",                       // ISO date or the vCard raw value
  "anniversary": "2015-06-20",
  "groups": ["Team", "Conference"],               // CATEGORIES membership
  "photo": "data:image/png;base64,iVBORw0KG…",    // raster data URI only (see below)
  "starred": true,
  "path": "/dav/addressbooks/user/default/8f14e45f.vcf" // CardDAV object path
}
```

- **`typedEmails`/`typedPhones` are authoritative when present**; `emails`/`phones`
  are then derived from them. A lean client may send only `emails`/`phones`.
- **`starred`** is stored as a reserved `CATEGORIES` value so it round-trips
  through CardDAV like any other group, but it is surfaced as a boolean and is
  **hidden from `/v1/contacts/groups`** — you cannot create or rename a group to
  the reserved starred name (the request is a `400`).
- **`photo`** is accepted **only** as a `data:` URI holding a PNG, JPEG, GIF or
  WebP, is content-sniffed (not trusted from the declared type), and is capped at
  **2 MiB**. An SVG, an HTML payload, or a bare `http(s)://` URL is dropped
  server-side rather than stored — so a hostile card can never carry a
  stored-XSS vector or a tracking beacon into a client that renders it.
- **Bounds** (silently clamped, never a `500`): 1024 chars per scalar field, 8192
  for `note`, 512 per email/phone/URL/IM value, 64 items per typed collection,
  128 groups per card, 128 chars per group name, 64 chars per `type` label.
- `POST` ignores any client-sent `uid`/`path` and always mints a fresh UID.
  Both `POST` and `PUT` require at least a name **or** one email (`400` otherwise).

#### Photos

`POST /v1/contacts/:uid/photo` takes a multipart form with a `file` field, runs
the same raster/size gate as the JSON path, attaches the result to the card, and
returns the saved `{ contact }`. `DELETE` clears it and likewise returns the saved
card. An over-cap upload is `413`; a non-raster image is `415`; an unknown `uid`
is `404`.

#### Groups (labels)

Groups are modelled **entirely** inside each card's `CATEGORIES` property (the
Google/Apple convention) — there is no separate group store, so a group exists
iff some card in the book carries it, and group state is per-account by
construction.

- **Assign / unassign is done through the normal contact `PUT`**: edit
  `contact.groups` and save. There is deliberately no assign endpoint.
- `POST /v1/contacts/groups` makes a brand-new (empty) group visible by writing a
  hidden placeholder card carrying only that category. Placeholder cards are
  filtered out of `/v1/contacts/cards`, `/v1/contacts/export` and group counts,
  and are deleted automatically once the group is removed from them. A duplicate
  name (case-insensitive) is `409`.
- `PATCH` rewrites the category on every card that carries it and reports how many
  cards changed; `DELETE` drops the membership from every card but **keeps the
  cards themselves**.

#### Import / export

`POST /v1/contacts/import` accepts a `.vcf` (one or many vCards) or a `.csv`
(Google/Outlook export shape). `format` may be `vcf` or `csv`; when omitted it is
sniffed from the filename, then from the content (`BEGIN:VCARD`). `mapping` is an
optional `field:index,field:index` override applied on top of the auto-matched CSV
header — useful after the client previews the header row. Recognised mapping
fields: `name`, `first`, `middle`, `last`, `prefix`, `suffix`, `nickname`,
`email`, `phone`, `org`, `department`, `title`, `note`, `website`, `birthday`,
`groups`, `starred`, `photo`.

Import is **bounded and forgiving**: the upload is capped at **10 MiB** (`413`
past that), at most **5000** contacts are created per request, every row is
sanitized through the same gate as a JSON write, and a malformed or
identity-less row is counted in `skipped` rather than failing the whole import.
Imported UIDs are always discarded and re-minted. The response is
`{ "imported": <n>, "skipped": <n> }`.

`GET /v1/contacts/export?format=vcf` (default) streams `text/vcard`;
`format=csv` streams `text/csv` with the fixed column set *Name, Given Name,
Family Name, Nickname, Organization, Department, Title, E-mail 1, Phone 1,
Website 1, Birthday, Notes, Groups, Starred, Photo* — a superset that re-imports
cleanly. CSV export is **formula-injection guarded**: any cell starting with
`=`, `+`, `-`, `@`, TAB or CR is prefixed with `'` so a hostile contact field
cannot execute when the export is opened in Excel/Sheets.

#### Frequently contacted

`GET /v1/contacts/frequent` is **not** a CardDAV read. It exposes the local
recent-recipients store that the send path already appends to after every send,
ordered by (`count` desc, `lastUsed` desc). It is read-only and per-account: the
store file is keyed by the request's own sanitized username under
`[cache] folder` — the exact path the send path writes to. Where there is no
local store (a brokered account whose sends were never recorded locally, or
`[cache] folder` unset) it returns an empty list rather than an error.

### Settings — vacation responder (`/v1/settings/vacation`)

Per-account out-of-office responder config. Durable-KV backed — returns `501`
when no store is wired. Owner is the authenticated identity (session email or
injected-credential mailbox); a caller only ever reads/writes **their own** config.

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `GET` | `/v1/settings/vacation` | — | `VacationConfig` |
| `PUT` | `/v1/settings/vacation` | `VacationConfig` | `VacationConfig` |

```jsonc
// VacationConfig
{
  "enabled": true,
  "subject": "Out of office",       // becomes a real mail Subject → CR/LF/NUL rejected (400)
  "body": "<p>Back Monday</p>",     // HTML, sanitized server-side (script/handlers/js: stripped)
  "startAt": "2026-07-10T00:00:00Z", // optional RFC3339; responder inactive before this
  "endAt":   "2026-07-20T00:00:00Z", // optional RFC3339; inactive after this; must be ≥ startAt
  "respondOnlyToContacts": false     // limit auto-replies to known contacts (anti-backscatter)
}
```

An enabled responder requires a non-empty `subject`. The body is sanitized (a
stored-XSS payload cannot ride the auto-reply). Loop/backscatter protection is
built in: auto-replies are never sent to another auto-reply (`Auto-Submitted`),
to list mail (`List-*` / `Precedence: bulk`), or to a null/bounce sender.

**Enforcement note:** lilmail is a **client** — it connects to the user's provider
over IMAP/SMTP and does **not** run the inbound delivery path, so storing
`enabled:true` here does not by itself make the provider auto-reply. This endpoint
is the authoritative **config** the client edits and stores; actual enforcement
must be set on the provider's own vacation/out-of-office feature (Gmail, Fastmail,
a self-hosted Dovecot/Sieve, …). The GET always echoes the stored config so the UI
stays truthful about what it holds.

### Settings — signatures (`/v1/settings/signatures`)

Multiple named HTML signatures. `PUT` replaces the whole set. Each signature's
HTML is sanitized. The server assigns an `id` when omitted (create by omitting).
At most one signature may be `default:true`.

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `GET` | `/v1/settings/signatures` | — | `{ signatures: Signature[] }` |
| `PUT` | `/v1/settings/signatures` | `{ signatures: Signature[] }` | `{ signatures: Signature[] }` |

```jsonc
// Signature
{ "id": "a1b2c3d4", "name": "Work", "html": "<b>Jane Doe</b>", "default": true }
```

### Settings — send-as identities (`/v1/settings/identities`)

The From/identity list the compose window offers. The primary mailbox is **always**
returned first with `isPrimary:true` (never removable), followed by any send-as
aliases. Each identity may link a default signature by id.

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `GET` | `/v1/settings/identities` | — | `{ identities: Identity[] }` |
| `PUT` | `/v1/settings/identities` | `{ identities: Identity[] }` | `{ identities: Identity[] }` |

`PUT` **replaces the whole set of aliases**. The primary mailbox is implicit: it is
never stored, never writable, and always re-added on read.

**lilmail is a client, not the send-as authority.** These identities are the
**client's read model** — the list the compose window offers. The user's own
provider SMTP server remains the authority for what From it will actually accept,
and re-checks it at submission time. Each address is still validated locally for
shape and header-injection (CR/LF/NUL in the address or name ⇒ `400`) before it is
stored. Nothing here makes an address inbound-deliverable — inbound delivery is the
provider's concern, not lilmail's.

Compose honours the choice: `POST /v1/messages` (and `/v1/drafts`) accept
`"from": "<address>"`, which must be the primary mailbox or a **registered**
identity — anything else is `403` (and the provider's SMTP server re-checks it at
submission). A scheduled send fires with that From but the record stays **owned**
by the authenticated mailbox.

```jsonc
// Identity
{ "address": "me@example.com", "name": "Me", "isPrimary": true, "defaultSignatureId": "a1b2c3d4" }
```

### Connected accounts + unified inbox (`/v1/accounts`, `/v1/unified`)

Additional mailboxes the user has connected, and a merged read across all of them.
Durable-KV backed (`501` without a store). Credentials are **AES-GCM encrypted at
rest** with the app key and are **never** returned in any response. Strict
per-user isolation: a user lists/adds/removes/reads only their own accounts;
another user's account is `404` (no-leak).

| Method | Path | Body | Returns |
|--------|------|------|---------|
| `GET`    | `/v1/accounts`         | — | `{ accounts: ConnectedAccount[] }` (no secrets) |
| `POST`   | `/v1/accounts`         | `AddAccount` | `201 ConnectedAccount` |
| `DELETE` | `/v1/accounts/:email`  | — | `204` (own) / `404` (foreign or missing) |
| `GET`    | `/v1/unified`          | `?folder=&limit=` | `{ folder, messages: Email[], errors: [] }` |
| `GET`    | `/v1/messages?account=all` | `?folder=&limit=` | same as `/v1/unified` (alias) |

```jsonc
// AddAccount (POST body) — password validated against the live IMAP server first
{ "email":"work@corp.com", "password":"…", "label":"Work", "color":"#0a0",
  "imapServer":"imap.corp.com", "imapPort":993, "smtpServer":"smtp.corp.com", "smtpPort":587 }

// ConnectedAccount (response) — password fields OMITTED, never serialized
{ "email":"work@corp.com", "label":"Work", "color":"#0a0",
  "imapServer":"imap.corp.com", "imapPort":993, "smtpServer":"smtp.corp.com", "smtpPort":587 }
```

The unified fetch runs one connection per account concurrently (each with its own
timeout); **one failing account never breaks the others** — its failure appears in
the `errors` array (`{account, error}`) alongside the messages that did load. Each
merged message is tagged with its source via `accountEmail` / `accountLabel` /
`accountColor` on the `Email` shape, and the list is newest-first, capped at 200.

### Examples

```bash
# List mailboxes
curl -b cookies.txt http://localhost:3000/v1/folders

# 50 most recent messages in INBOX
curl -b cookies.txt 'http://localhost:3000/v1/messages?folder=INBOX&limit=50'

# Read one message
curl -b cookies.txt 'http://localhost:3000/v1/messages/42?folder=INBOX'

# Full-text search
curl -b cookies.txt 'http://localhost:3000/v1/search?folder=INBOX&q=invoice'

# Mark as read (\Seen)  /  star (\Flagged)
curl -b cookies.txt -X PATCH 'http://localhost:3000/v1/messages/42/flags?folder=INBOX' \
  -H 'Content-Type: application/json' -d '{"flag":"\\Seen","add":true}'

# Delete (moves to Trash by default)
curl -b cookies.txt -X DELETE 'http://localhost:3000/v1/messages/42?folder=INBOX'

# Permanently delete (expunge, skip Trash)
curl -b cookies.txt -X DELETE 'http://localhost:3000/v1/messages/42?folder=INBOX&hard=true'

# Move / archive a message
curl -b cookies.txt -X POST 'http://localhost:3000/v1/messages/42/move?folder=INBOX' \
  -H 'Content-Type: application/json' -d '{"toFolder":"Archive"}'

# Send a message
curl -b cookies.txt -X POST http://localhost:3000/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"to":"alice@example.com","subject":"Hi","text":"Hello from /v1"}'

# Save a draft
curl -b cookies.txt -X POST http://localhost:3000/v1/drafts \
  -H 'Content-Type: application/json' \
  -d '{"to":"alice@example.com","subject":"WIP","text":"unfinished…"}'

# List calendar events for a range (CalDAV must be enabled)
curl -b cookies.txt 'http://localhost:3000/v1/calendar/events?start=2026-06-01T00:00:00Z&end=2026-07-01T00:00:00Z'

# Search contacts (CardDAV must be enabled)
curl -b cookies.txt 'http://localhost:3000/v1/contacts?q=alice'

# Set the vacation responder
curl -b cookies.txt -X PUT http://localhost:3000/v1/settings/vacation \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"subject":"Out of office","body":"<p>Back Monday</p>"}'

# Add a connected account (password is validated against IMAP, then encrypted at rest)
curl -b cookies.txt -X POST http://localhost:3000/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{"email":"work@corp.com","password":"…","label":"Work","imapServer":"imap.corp.com"}'

# Unified inbox across the primary + all connected accounts
curl -b cookies.txt 'http://localhost:3000/v1/unified?folder=INBOX&limit=50'
```

### `Email` shape (abridged)

```jsonc
{
  "id": "42",
  "from": "alice@example.com",
  "fromName": "Alice",
  "to": "me@example.com",
  "toNames": ["Me"],
  "cc": "bob@example.com",
  "subject": "Invoice",
  "preview": "Here is the invoice you asked for…",
  "body": "plain text body",
  "html": "<p>…</p>",
  "date": "2026-06-26T10:00:00Z",
  "hasAttachments": true,
  "attachments": [
    {
      "id": "SU5CT1gAMzQAMi4x",   // opaque attachment identifier
      "partId": "2.1",             // IMAP MIME part path — use with the /v1 download route
      "filename": "invoice.pdf",
      "contentType": "application/pdf",
      "size": 84320,
      "isInline": false
    }
  ],
  "flags": ["\\Seen"],
  "messageId": "<…@example.com>",
  "inReplyTo": "<…>",
  "references": ["<…>"],
  "accountEmail": "work@corp.com",   // only in unified results — source account tag
  "accountLabel": "Work",
  "accountColor": "#0a0",
  "auth": {                           // only on a single-message read; omitted if no header
    "spf": "pass",                    // pass|fail|softfail|neutral|none|temperror|permerror
    "dkim": "pass",
    "dmarc": "pass",
    "dkimDomain": "sender.com",
    "raw": "mx.example.com; spf=pass …" // verbatim Authentication-Results value
  },
  "invite": { … },                    // present iff the message carries a text/calendar iMIP part
  "unsubscribe": {                    // present iff the message advertises List-Unsubscribe
    "httpUrl": "https://example.com/u/abc",
    "mailtoUrl": "mailto:unsub@example.com?subject=unsub",
    "oneClick": true
  },
  "brand": {                          // present iff DMARC passed AND the domain publishes BIMI
    "domain": "sender.com",
    "logo": "data:image/svg+xml;base64,…",
    "vmc": true
  }
}
```

`invite` (nil for ordinary mail) is the parsed iTIP/iMIP invitation — attendees
plus the recipient's own `MyPartStat` — and is what the client answers with
`POST /v1/calendar/rsvp`.

`unsubscribe` is the parsed `List-Unsubscribe` (RFC 2369) pair. **lilmail never
unsubscribes on your behalf** — it only surfaces the targets. Only `http`,
`https` and `mailto` schemes are ever emitted; anything else is dropped so a
hostile scheme cannot ride through to the client. `oneClick` is true only when
the sender also sent `List-Unsubscribe-Post: List-Unsubscribe=One-Click` (RFC
8058) **and** an `httpUrl` exists, meaning the client may POST
`List-Unsubscribe=One-Click` to `httpUrl` directly.

`brand` is a **verified** sender brand logo (BIMI) and is **fail-closed**: it is
populated only when the message carries a DMARC `pass` verdict *and* the From
domain publishes a BIMI record whose logo lilmail fetched (SSRF-screened) and
sanitized. An unauthenticated sender therefore never gets a logo, so its presence
is only ever a positive trust signal, never a phishing aid. `logo` is a sanitized
SVG `data:` URI safe to place in an `<img src>`. `vmc` reflects only that the
record referenced a Verified Mark Certificate (`a=` tag) — the certificate chain
is **not** validated, so treat it as informational. Resolution is served from a
per-domain cache and warmed in the background on a miss, so the first read of a
message from a new domain may return no `brand` even though one exists; a
subsequent read returns it.

`attachments[]` is metadata only (no bytes). Build a download link as
`/v1/messages/{id}/attachments/{partId}?folder={folder}`. `isInline` flags parts
meant to render inline (e.g. embedded images) versus regular file attachments.

`auth` (present on a single-message read, `GET /v1/messages/:uid`) surfaces the
**receiving server's** SPF/DKIM/DMARC verdict, parsed read-only from the message's
`Authentication-Results` header (RFC 8601). lilmail does not re-verify; it exposes
the trusted receiver's stamp so the client can render a "verified sender" / "why in
spam" badge. `null`/absent when the message carries no such header.

## Demo mode

When `[demo] enabled = true`, the API is backed by the in-memory `DemoClient`
(no IMAP contact), so it returns seeded data — useful for building/screenshotting
clients without a live mailbox.

## Not yet exposed

Recurring-event expansion (server-side RRULE materialization) is not yet exposed
over `/v1`. Track this in [ROADMAP.md](../ROADMAP.md).
