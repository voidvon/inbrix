package api

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func smtpTLSListener(t *testing.T) net.Listener {
	t.Helper()
	// Reuse Go's localhost test certificate, without sending any HTTP traffic.
	certServer := httptest.NewTLSServer(nil)
	config := certServer.TLS.Clone()
	certServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return tls.NewListener(listener, config)
}

func TestSMTPDeadlineRemainsAfterGreeting(t *testing.T) {
	listener := smtpTLSListener(t)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "220 localhost ready\r\n")
		_, _ = io.Copy(io.Discard, conn) // Never answer the next SMTP command.
	}()
	client := NewSMTPClient("127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "alice@example.test", "secret", false)
	client.SetInsecureSkipVerify(true)
	smtpClient, err := client.connect(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer smtpClient.Close()
	err = smtpClient.Noop()
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("SMTP command error = %v, want timeout after greeting", err)
	}
}

func TestSMTPAcceptedMessageDoesNotFailWhenQuitDisconnects(t *testing.T) {
	for _, raw := range []bool{true, false} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			listener := smtpTLSListener(t)
			serverResult := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					serverResult <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = io.WriteString(conn, "220 localhost ready\r\n")
				reader := textproto.NewReader(bufio.NewReader(conn))
				accepted := false
				for {
					line, err := reader.ReadLine()
					if err != nil {
						serverResult <- err
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						_, _ = io.WriteString(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(line, "AUTH"):
						_, _ = io.WriteString(conn, "235 authenticated\r\n")
					case line == "DATA":
						_, _ = io.WriteString(conn, "354 send data\r\n")
						if _, err := io.Copy(io.Discard, reader.DotReader()); err != nil {
							serverResult <- err
							return
						}
						accepted = true
						_, _ = io.WriteString(conn, "250 accepted\r\n")
					case line == "QUIT":
						if !accepted {
							serverResult <- fmt.Errorf("QUIT before message acceptance")
						} else {
							serverResult <- nil
						}
						return // Drop the connection instead of answering QUIT.
					default:
						_, _ = io.WriteString(conn, "250 OK\r\n")
					}
				}
			}()
			client := NewSMTPClient("127.0.0.1", listener.Addr().(*net.TCPAddr).Port, "alice@example.test", "secret", false)
			client.SetInsecureSkipVerify(true)
			var err error
			if raw {
				err = client.SendRawMessage([]string{"bob@example.test"}, []byte("Subject: Test\r\n\r\nTest body\r\n"))
			} else {
				err = client.SendMail("bob@example.test", "Test", "Test body", nil)
			}
			if err != nil {
				t.Fatalf("accepted send reported failure: %v", err)
			}
			if err := <-serverResult; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSMTPConnectBoundsStalledGreetingAndTLS(t *testing.T) {
	for _, startTLS := range []bool{true, false} {
		t.Run(fmt.Sprintf("STARTTLS=%t", startTLS), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				// Accept TCP but never provide a greeting or TLS handshake.
				_, _ = io.Copy(io.Discard, conn)
			}()
			addr := listener.Addr().(*net.TCPAddr)
			client := NewSMTPClient("127.0.0.1", addr.Port, "alice@example.test", "secret", startTLS)
			began := time.Now()
			_, err = client.connect(50 * time.Millisecond)
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Fatalf("connect error = %v, want network timeout", err)
			}
			if time.Since(began) > time.Second {
				t.Fatal("SMTP connection did not respect its deadline")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("timed-out SMTP connection was not closed")
			}
		})
	}
}

func TestSMTPClientAuthenticationUsername(t *testing.T) {
	client := NewSMTPClient("smtp.example.com", 465, "alice@example.com", "secret", false)
	if got := client.Transport().AuthUsername; got != "alice@example.com" {
		t.Fatalf("default auth username = %q, want full email address", got)
	}

	client.SetAuthUsername(" alice ")
	if got := client.Transport().AuthUsername; got != "alice" {
		t.Fatalf("overridden auth username = %q, want %q", got, "alice")
	}

	client.SetAuthUsername("  ")
	if got := client.Transport().AuthUsername; got != "alice" {
		t.Fatalf("blank override changed auth username to %q", got)
	}
}

func TestSMTPDataAcknowledgementDistinguishesRejectionFromLostResponse(t *testing.T) {
	if errors.Is(smtpDataCloseError(&textproto.Error{Code: 550, Msg: "rejected"}), ErrSMTPDeliveryUnknown) {
		t.Fatal("SMTP rejection reported as uncertain")
	}
	if !errors.Is(smtpDataCloseError(io.EOF), ErrSMTPDeliveryUnknown) {
		t.Fatal("lost final response reported as definite failure")
	}
}
