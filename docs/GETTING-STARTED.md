# Getting Started with lilmail

lilmail is a single Go binary — there is no build step for the frontend and no
database to provision. This guide covers installation, first-run, and the most
common configuration scenarios.

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| Go 1.25+ | Only needed to build from source (`go.mod` requires `go 1.25.0`) |
| IMAP server | Any IMAP4rev1 server (port 993 TLS or 143 STARTTLS) |
| SMTP server | Any SMTP server (port 587 STARTTLS or 465 implicit TLS) |

A pre-built binary is available on the
[latest release](https://github.com/vul-os/lilmail/releases/latest) page — no Go
installation required to run it.

## Installation

### Option A — pre-built binary

1. Download the archive for your OS and CPU from the
   [latest release](https://github.com/vul-os/lilmail/releases/latest) — macOS
   and Linux, `amd64` and `arm64`, plus a source zip and a `SHA256SUMS` manifest
   covering every asset.
2. Extract the archive to a directory of your choice.
3. Copy `config.toml.example` to `config.toml` in the same directory (or use
   the example below).
4. Edit `config.toml` with your mail server details.
5. Run the binary: `./lilmail`

### Option B — build from source

```bash
git clone https://github.com/vul-os/lilmail.git
cd lilmail
go build -o lilmail
```

## Minimal configuration

Create `config.toml` in the same directory as the binary:

```toml
[server]
port = 3000
username_is_email = true

[imap]
server = "imap.example.com"
port   = 993
tls    = true

[smtp]
server       = "smtp.example.com"
port         = 587
use_starttls = true

[cache]
folder = "./cache"

[jwt]
secret = "change-me-to-a-long-random-string"

[encryption]
key = "a-32-character-encryption-key!!"
```

> **Security note:** generate a strong `jwt.secret` (e.g.
> `openssl rand -hex 32`) and a random 32-byte `encryption.key` before
> deploying. Both protect session tokens and stored credentials at rest.

## First run

```bash
./lilmail
# or: go run main.go
```

Open **http://localhost:3000** in your browser. You will see the login page.
Enter your email address and mail server password — or click **Sign in with
OAuth2** if OAuth2 is configured.

## Common scenarios

### Self-signed TLS certificate

Set `insecure_skip_verify = true` under `[smtp]` (IMAP uses standard TLS on
port 993 and does not expose this flag):

```toml
[smtp]
insecure_skip_verify = true
```

### HTTPS

lilmail does not terminate TLS. It serves plain HTTP on `[server] port` and
nothing else, so HTTPS means a reverse proxy in front of it (nginx, Caddy,
Traefik — whatever you already run). Point the proxy at `http://127.0.0.1:3000`
and give it the certificate.

Then tell lilmail it is being served over HTTPS, so the session and CSRF
cookies get the `Secure` flag:

```toml
[server]
secure_cookies = true
```

`[ssl]` is **not** how you serve HTTPS — it only validates a certificate pair
at startup and switches on the `Strict-Transport-Security` header. See
[the `[ssl]` section](CONFIGURATION.md#ssl) before enabling it.

### OAuth2 / OpenID Connect

```toml
[oauth2]
enabled      = true
client_id    = "lilmail"
client_secret = "your-client-secret"
auth_url     = "https://auth.example.com/o/authorize/"
token_url    = "https://auth.example.com/o/token/"
redirect_url = "https://yourdomain.com/auth/oauth/callback"
scopes       = ["openid", "email", "profile"]
mechanism    = "xoauth2"
use_pkce     = true
```

Register `https://yourdomain.com/auth/oauth/callback` as a redirect URI with
your identity provider. Password login continues to work alongside OAuth2.

### Run behind a reverse proxy (nginx / Caddy)

Let the proxy handle TLS termination and set `[server] secure_cookies = true`.

**lilmail does not read `X-Forwarded-For` or any other `X-Forwarded-*` header.**
No proxy-header trust is configured, so the client address it sees is whatever
connected to it directly — behind a proxy, that is the proxy. The practical
consequence is that the per-IP login, send and AI rate limits collapse into
**global** limits. If you need per-client limiting behind a proxy, enforce it in
the proxy.

## Verifying the installation

```bash
curl http://localhost:3000/health
# OK
```

The `/health` endpoint returns `200` with the plain-text body `OK` when the
server is running. It is not JSON. It does not require authentication.

## Next steps

- Full configuration reference: [CONFIGURATION.md](CONFIGURATION.md)
- Code layout and architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Feature roadmap: [../ROADMAP.md](../ROADMAP.md)
