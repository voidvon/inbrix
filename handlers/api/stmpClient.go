package api

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"
)

// SMTPClient handles email sending
type SMTPClient struct {
	server             string
	port               int
	email              string
	authUsername       string
	password           string
	useOAuth           bool
	token              string
	mechanism          string // "xoauth2" or "oauthbearer"
	insecureSkipVerify bool   // if true, skip TLS certificate verification
	useStartTLS        bool   // true = STARTTLS (port 587); false = implicit TLS (port 465)
}

// NewSMTPClient creates a new SMTP client using password authentication.
// useStartTLS controls the connection mode: true → plaintext + STARTTLS
// (port 587), false → implicit TLS from the start (port 465).
func NewSMTPClient(server string, port int, email, password string, useStartTLS bool) *SMTPClient {
	return &SMTPClient{
		server:       server,
		port:         port,
		email:        email,
		authUsername: email,
		password:     password,
		useStartTLS:  useStartTLS,
	}
}

// NewSMTPClientOAuth creates a new SMTP client authenticated with an OAuth2
// access token (XOAUTH2 or OAUTHBEARER).
func NewSMTPClientOAuth(server string, port int, email, token, mechanism string, useStartTLS bool) *SMTPClient {
	return &SMTPClient{
		server:       server,
		port:         port,
		email:        email,
		authUsername: email,
		token:        token,
		mechanism:    mechanism,
		useOAuth:     true,
		useStartTLS:  useStartTLS,
	}
}

// SetAuthUsername overrides the SMTP authentication identity while preserving
// email as the envelope sender. Most hosted providers require the full email
// address; self-hosted servers may instead use a bare account name.
func (c *SMTPClient) SetAuthUsername(username string) {
	if username = strings.TrimSpace(username); username != "" {
		c.authUsername = username
	}
}

// SetInsecureSkipVerify controls whether TLS certificate verification is skipped.
// Use only for self-signed or development servers; default is false (verify certs).
func (c *SMTPClient) SetInsecureSkipVerify(skip bool) {
	c.insecureSkipVerify = skip
}

// SMTPTransport is a snapshot of an SMTPClient's connection parameters, including
// the authentication secret (password or OAuth token). It lets a caller persist
// everything needed to reconnect + send later (e.g. a scheduled send) WITHOUT
// duplicating the credential-derivation logic in the handler layer. The secret is
// sensitive: callers MUST store it encrypted at rest, never in plaintext.
type SMTPTransport struct {
	Server       string
	Port         int
	Email        string
	AuthUsername string
	UseOAuth     bool
	Mechanism    string
	UseSTARTTLS  bool
	InsecureSkip bool
	// Secret is the password (plain auth) or the OAuth access token (oauth auth).
	Secret string
}

// Transport returns a snapshot of this client's connection parameters + secret,
// for callers that need to persist a delayed send. Reusing this keeps the
// broker/session/oauth credential logic in ONE place (whoever built the client).
func (c *SMTPClient) Transport() SMTPTransport {
	secret := c.password
	if c.useOAuth {
		secret = c.token
	}
	return SMTPTransport{
		Server:       c.server,
		Port:         c.port,
		Email:        c.email,
		AuthUsername: c.authUsername,
		UseOAuth:     c.useOAuth,
		Mechanism:    c.mechanism,
		UseSTARTTLS:  c.useStartTLS,
		InsecureSkip: c.insecureSkipVerify,
		Secret:       secret,
	}
}

// Bound the whole SMTP exchange, including greeting/TLS/DATA/QUIT. Without a
// deadline, a provider that accepts TCP but stops responding pins the HTTP send
// request (and its browser spinner) indefinitely.
const smtpSendTimeout = 2 * time.Minute

// ErrSMTPDeliveryUnknown means the connection disappeared while waiting for
// the final DATA acknowledgement. The server may already have accepted mail.
var ErrSMTPDeliveryUnknown = errors.New("SMTP acceptance could not be confirmed")

func smtpDataCloseError(err error) error {
	var rejection *textproto.Error
	if errors.As(err, &rejection) {
		return fmt.Errorf("close failed: %w", err)
	}
	return fmt.Errorf("%w: close failed: %v", ErrSMTPDeliveryUnknown, err)
}

func (c *SMTPClient) connect(timeout time.Duration) (*smtp.Client, error) {
	addr := net.JoinHostPort(c.server, fmt.Sprint(c.port))
	dialer := net.Dialer{Timeout: min(timeout, 15*time.Second)}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("smtp dial failed: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp deadline failed: %w", err)
	}
	tlsCfg := &tls.Config{
		ServerName:         c.server,
		InsecureSkipVerify: c.insecureSkipVerify, //nolint:gosec // operator-controlled
	}
	if !c.useStartTLS {
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("smtp TLS handshake failed: %w", err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, c.server)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp greeting failed: %w", err)
	}
	if c.useStartTLS {
		if err := client.Hello(GetDomainFromEmail(c.email)); err != nil {
			client.Close()
			return nil, fmt.Errorf("smtp hello failed: %w", err)
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			client.Close()
			return nil, fmt.Errorf("smtp STARTTLS failed: %w", err)
		}
	}
	return client, nil
}

// MailOptions carries optional RFC 2822 header fields for a message.
type MailOptions struct {
	// Cc is a comma-separated list of CC recipients.
	Cc string
	// Bcc is a comma-separated list of BCC recipients (added as RCPT TO, not in headers).
	Bcc string
	// InReplyTo is the Message-ID of the message being replied to.
	InReplyTo string
	// References is the full References header value for threading.
	References string
}

// SendMail sends an email using SMTP.  Extra recipients and threading headers
// can be provided via opts (pass nil or &MailOptions{} for a plain send).
func (c *SMTPClient) SendMail(to, subject, body string, opts *MailOptions) error {
	if opts == nil {
		opts = &MailOptions{}
	}

	// Header-injection guard for the values written verbatim into the DATA header
	// block below. A CR/LF/NUL in any of them would terminate the header line and
	// allow header smuggling (e.g. an injected Bcc:) or message splitting. Fail
	// closed before opening the connection.
	for _, v := range []string{to, subject, opts.Cc, opts.InReplyTo, opts.References} {
		if err := validateHeaderValue(v); err != nil {
			return fmt.Errorf("smtp: refusing to send message with unsafe header: %w", err)
		}
	}

	client, err := c.connect(smtpSendTimeout)
	if err != nil {
		return err
	}
	defer client.Close()

	domain := GetDomainFromEmail(c.email)
	username := c.authUsername

	// Authenticate.
	var auth smtp.Auth
	if c.useOAuth {
		switch strings.ToLower(c.mechanism) {
		case "oauthbearer":
			auth = NewSMTPOAuthBearer(c.email, c.token, c.server, c.port)
		default:
			auth = NewSMTPXoauth2(c.email, c.token)
		}
	} else {
		auth = smtp.PlainAuth("", username, c.password, c.server)
	}
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("auth failed: %v", err)
	}

	// Set sender.
	if err = client.Mail(c.email); err != nil {
		return fmt.Errorf("mail from failed: %v", err)
	}

	// Primary recipients.
	for _, rcpt := range splitAddresses(to) {
		if err = client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %q failed: %v", rcpt, err)
		}
	}
	// CC recipients (visible in headers).
	for _, rcpt := range splitAddresses(opts.Cc) {
		if err = client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt cc %q failed: %v", rcpt, err)
		}
	}
	// BCC recipients (RCPT TO only — not added to message headers).
	for _, rcpt := range splitAddresses(opts.Bcc) {
		if err = client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt bcc %q failed: %v", rcpt, err)
		}
	}

	// Write DATA section.
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("data failed: %v", err)
	}

	now := time.Now().Format(time.RFC822Z)
	msgID := fmt.Sprintf("<%s@%s>", generateMessageID(), domain)

	var hdr strings.Builder
	hdr.WriteString("Date: " + now + "\r\n")
	hdr.WriteString("From: " + username + " <" + c.email + ">\r\n")
	hdr.WriteString("To: " + to + "\r\n")
	if opts.Cc != "" {
		hdr.WriteString("Cc: " + opts.Cc + "\r\n")
	}
	hdr.WriteString("Subject: " + subject + "\r\n")
	hdr.WriteString("MIME-Version: 1.0\r\n")
	hdr.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	hdr.WriteString("Message-ID: " + msgID + "\r\n")
	if opts.InReplyTo != "" {
		hdr.WriteString("In-Reply-To: " + opts.InReplyTo + "\r\n")
	}
	if opts.References != "" {
		hdr.WriteString("References: " + opts.References + "\r\n")
	}
	hdr.WriteString("\r\n")
	hdr.WriteString(body)

	if _, err = writer.Write([]byte(hdr.String())); err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if err = writer.Close(); err != nil {
		return smtpDataCloseError(err)
	}
	// DATA's final 250 confirms acceptance. A failed QUIT cannot undo it and
	// must not report a failed send that encourages a duplicate retry.
	_ = client.Quit()
	return nil
}

// SendRawMessage sends a pre-built RFC 2822 message via SMTP. The caller is
// responsible for constructing the full message including all headers and body.
// allRcpts is the union of To, CC, and BCC addresses to use as RCPT TO.
func (c *SMTPClient) SendRawMessage(allRcpts []string, rawMessage []byte) error {
	return c.SendRawMessageReader(allRcpts, bytes.NewReader(rawMessage))
}

// SendRawMessageReader sends a pre-built message without first materializing it
// as a byte slice. The SMTP DATA writer handles dot-stuffing while io.Copy keeps
// memory usage bounded for messages backed by a temporary file.
func (c *SMTPClient) SendRawMessageReader(allRcpts []string, rawMessage io.Reader) error {
	smtpClient, err := c.connect(smtpSendTimeout)
	if err != nil {
		return err
	}
	defer smtpClient.Close()

	username := c.authUsername
	var auth smtp.Auth
	if c.useOAuth {
		switch strings.ToLower(c.mechanism) {
		case "oauthbearer":
			auth = NewSMTPOAuthBearer(c.email, c.token, c.server, c.port)
		default:
			auth = NewSMTPXoauth2(c.email, c.token)
		}
	} else {
		auth = smtp.PlainAuth("", username, c.password, c.server)
	}
	if err = smtpClient.Auth(auth); err != nil {
		return fmt.Errorf("auth failed: %v", err)
	}

	if err = smtpClient.Mail(c.email); err != nil {
		return fmt.Errorf("mail from failed: %v", err)
	}
	for _, rcpt := range allRcpts {
		if err = smtpClient.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt to %q failed: %v", rcpt, err)
		}
	}

	writer, err := smtpClient.Data()
	if err != nil {
		return fmt.Errorf("data failed: %v", err)
	}
	if _, err = io.Copy(writer, rawMessage); err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	if err = writer.Close(); err != nil {
		return smtpDataCloseError(err)
	}
	_ = smtpClient.Quit()
	return nil
}

// splitAddresses splits a comma-separated address list into individual entries,
// trimming whitespace and skipping empty strings.
func splitAddresses(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, a := range strings.Split(s, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			result = append(result, a)
		}
	}
	return result
}

// generateMessageID creates a unique Message-ID for the email
func generateMessageID() string {
	return fmt.Sprintf("%d.%d.%d",
		time.Now().UnixNano(),
		os.Getpid(),
		rand.Int63())
}
