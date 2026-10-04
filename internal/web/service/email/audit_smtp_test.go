package email

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

func auditSMTPSettings(t *testing.T, addr, mode string) *EmailService {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "panel.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	s := service.SettingService{}
	for _, err := range []error{s.SetSmtpHost(host), s.SetSmtpPort(port), s.SetSmtpFrom("panel@example.org"), s.SetSmtpTo(`"Ops, team" <ops@example.org>`), s.SetSmtpEncryptionType(mode)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return NewEmailService(s)
}

func auditSMTPServer(t *testing.T, advertiseTLS bool) (string, func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	var lines []string
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprint(conn, "220 audit SMTP\r\n")
		reader := bufio.NewReader(conn)
		data := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
			if data {
				if line == "." {
					data = false
					fmt.Fprint(conn, "250 queued\r\n")
				}
				continue
			}
			switch {
			case strings.HasPrefix(line, "EHLO") && advertiseTLS:
				fmt.Fprint(conn, "250-audit\r\n250 STARTTLS\r\n")
			case line == "STARTTLS":
				fmt.Fprint(conn, "454 TLS unavailable\r\n")
			case line == "DATA":
				data = true
				fmt.Fprint(conn, "354 continue\r\n")
			default:
				fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

func TestAuditSMTPTransportMatchesTestAndSend(t *testing.T) {
	for _, operation := range []string{"test", "send"} {
		for _, mode := range []string{"none", "starttls"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				addr, lines := auditSMTPServer(t, mode == "none")
				svc := auditSMTPSettings(t, addr, mode)
				if operation == "test" {
					result := svc.TestConnection()
					if result.Success != (mode == "none") {
						t.Fatalf("test result: %+v", result)
					}
					if mode == "starttls" && result.Message != "smtpErrorStarttls" {
						t.Fatalf("wrong STARTTLS error: %+v", result)
					}
				} else {
					err := svc.Send("subject", "body")
					if (err == nil) != (mode == "none") {
						t.Fatalf("send error = %v", err)
					}
				}
				commands := strings.Join(lines(), "\n")
				if mode == "none" {
					if strings.Contains(commands, "STARTTLS") || !strings.Contains(commands, "RCPT TO:<ops@example.org>") {
						t.Fatalf("wrong plaintext/envelope behavior: %s", commands)
					}
				} else if strings.Contains(commands, "MAIL FROM") || strings.Contains(commands, "DATA") {
					t.Fatal("required TLS silently downgraded")
				}
			})
		}
	}
}

func TestAuditSMTPRejectsUnknownModeBeforeConnect(t *testing.T) {
	svc := auditSMTPSettings(t, "127.0.0.1:1", "unknown")
	if got := svc.TestConnection(); got.Success || got.Message != "smtpErrorUnknown" {
		t.Fatalf("unknown mode result: %+v", got)
	}
	if err := svc.Send("subject", "body"); err == nil || !strings.Contains(err.Error(), "encryption type") {
		t.Fatalf("unknown mode send: %v", err)
	}
}

func TestAuditSMTPDeadlineClosesConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	closed := make(chan int64, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		n, _ := io.Copy(io.Discard, conn) // Never send the SMTP greeting.
		closed <- n
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = sendSMTP(ctx, smtpSettings{addr: ln.Addr().String(), host: "localhost", encryption: "none", from: "from@example.org", recipients: []string{"to@example.org"}}, []byte("body"))
	if err == nil || classifySMTPError(err) != "smtpErrorTimeout" {
		t.Fatalf("deadline error = %v", err)
	}
	select {
	case n := <-closed:
		if n != 0 {
			t.Fatalf("sent %d bytes after stalled greeting", n)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out operation left its socket open")
	}
}

func TestAuditSMTPRecipientsAndDialTimeout(t *testing.T) {
	to, err := parseRecipients(`"Ops, team" <ops@example.org>, admin@example.org`)
	if err != nil || len(to) != 2 || to[0] != "ops@example.org" {
		t.Fatalf("recipient parse = %v, %v", to, err)
	}
	if _, err := parseRecipients("bad-address"); err == nil {
		t.Fatal("invalid recipient accepted")
	}
	err = &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
	if got := classifySMTPError(err); got != "smtpErrorTimeout" {
		t.Fatalf("dial timeout classified as %q", got)
	}
	if got := classifySMTPError(errors.New("dial tcp: i/o timeout")); got != "smtpErrorTimeout" {
		t.Fatalf("string timeout classified as %q", got)
	}
}
