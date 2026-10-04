package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

// EmailService sends email notifications via SMTP.
type EmailService struct {
	settingService service.SettingService
}

// SMTPTestResult holds the result of an SMTP connection test.
type SMTPTestResult struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"`   // "connect" | "auth" | "send"
	Message string `json:"message"` // classified error message
}

// NewEmailService creates a new EmailService.
func NewEmailService(settingService service.SettingService) *EmailService {
	return &EmailService{settingService: settingService}
}

// smtpConnectTimeout bounds dialing; smtpDeadline bounds the entire operation,
// including dialing and TLS. Tests can shorten the overall deadline.
const smtpConnectTimeout = 10 * time.Second

var smtpDeadline = 30 * time.Second

type smtpSettings struct {
	host, addr, username, password, from, fromName, encryption string
	recipients                                                 []string
}

// Use the same validated settings and transport for tests and notifications.
func (s *EmailService) settings() (smtpSettings, SMTPTestResult, error) {
	var cfg smtpSettings
	failure := func(stage, key, detail string) (smtpSettings, SMTPTestResult, error) {
		return cfg, SMTPTestResult{false, stage, key}, fmt.Errorf("%s", detail)
	}
	host, err := s.settingService.GetSmtpHost()
	if err != nil || host == "" {
		return failure("connect", "smtpHostNotConfigured", "smtp host not configured")
	}
	cfg.host = host
	port, err := s.settingService.GetSmtpPort()
	if err != nil || port <= 0 {
		port = 587
	}
	cfg.username, _ = s.settingService.GetSmtpUsername()
	cfg.password, _ = s.settingService.GetSmtpPassword()
	cfg.from, _ = s.settingService.GetSmtpFrom()
	cfg.fromName, _ = s.settingService.GetSmtpFromName()
	to, _ := s.settingService.GetSmtpTo()
	cfg.encryption, _ = s.settingService.GetSmtpEncryptionType()
	switch cfg.encryption {
	case "none", "starttls", "tls":
	default:
		return failure("connect", "smtpErrorUnknown", "unknown SMTP encryption type: "+cfg.encryption)
	}
	if cfg.from == "" {
		cfg.from = cfg.username
	}
	if cfg.from == "" {
		return failure("send", "smtpFromNotConfigured", "smtp from not configured")
	}
	parsed, err := mail.ParseAddress(cfg.from)
	if err != nil {
		return failure("send", "smtpFromNotConfigured", "invalid smtp from address")
	}
	cfg.from, cfg.fromName = resolveFrom(parsed.String(), cfg.fromName)
	cfg.recipients, err = parseRecipients(to)
	if err != nil {
		return failure("send", "smtpNoRecipients", "invalid SMTP recipients: "+err.Error())
	}
	if len(cfg.recipients) == 0 {
		return failure("send", "smtpNoRecipients", "no recipients configured")
	}
	cfg.addr = net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return cfg, SMTPTestResult{}, nil
}

// Send has one deadline for dialing, TLS and SMTP. Closing the socket on
// cancellation prevents delivery from continuing after a timeout is returned.
func (s *EmailService) Send(subject, body string) error {
	cfg, _, err := s.settings()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), smtpDeadline)
	defer cancel()
	_, err = sendSMTP(ctx, cfg, buildMessage(cfg.from, cfg.fromName, cfg.recipients, subject, body))
	return err
}

func (s *EmailService) TestConnection() SMTPTestResult {
	cfg, failure, err := s.settings()
	if err != nil {
		return failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), smtpDeadline)
	defer cancel()
	msg := buildMessage(cfg.from, cfg.fromName, cfg.recipients, "[3x-ui] Test email",
		`<html><body><h2>Test email from 3x-ui</h2><p>SMTP is configured correctly.</p></body></html>`)
	stage, err := sendSMTP(ctx, cfg, msg)
	if err != nil {
		return SMTPTestResult{false, stage, classifySMTPError(err)}
	}
	return SMTPTestResult{true, "send", "smtpTestSuccess"}
}

func sendSMTP(ctx context.Context, cfg smtpSettings, msg []byte) (stage string, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	dialer := &net.Dialer{Timeout: smtpConnectTimeout}
	var conn net.Conn
	if cfg.encryption == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: cfg.host}}).DialContext(ctx, "tcp", cfg.addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", cfg.addr)
	}
	if err != nil {
		return "connect", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return "connect", err
		}
	}
	client, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		return "auth", err
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return "auth", err
	}
	if cfg.encryption == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "auth", fmt.Errorf("SMTP server does not support required STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.host}); err != nil {
			return "auth", err
		}
	}
	if cfg.username != "" && cfg.password != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)); err != nil {
			return "auth", err
		}
	}
	if err := client.Mail(cfg.from); err != nil {
		return "send", err
	}
	for _, recipient := range cfg.recipients {
		if err := client.Rcpt(recipient); err != nil {
			return "send", err
		}
	}
	w, err := client.Data()
	if err != nil {
		return "send", err
	}
	if _, err := w.Write(msg); err != nil {
		return "send", err
	}
	return "send", w.Close()
}

// SendTest sends a test email and returns any error with detail.
func (s *EmailService) SendTest() error {
	return s.Send(
		"[3x-ui] Test email",
		`<html><body style="font-family:monospace;font-size:14px">
<h2>Test email from 3x-ui</h2>
<p>If you received this, SMTP is configured correctly.</p>
</body></html>`,
	)
}

// classifySMTPError maps a raw SMTP error to an i18n key. The key is returned
// unprefixed: the caller renders it under "pages.settings.", as does every
// other Message this file produces.
func classifySMTPError(err error) string {
	msg := err.Error()
	msgLower := strings.ToLower(msg)

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		(errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(msgLower, "timeout") {
		return "smtpErrorTimeout"
	}
	switch {
	case strings.Contains(msg, "535") || strings.Contains(msgLower, "authentication"):
		return "smtpErrorAuth"
	case strings.Contains(msg, "534") || strings.Contains(msgLower, "starttls"):
		return "smtpErrorStarttls"
	case strings.Contains(msg, "465") || strings.Contains(msgLower, "tls"):
		return "smtpErrorTls"
	case strings.Contains(msgLower, "connection refused") || strings.Contains(msgLower, "dial"):
		return "smtpErrorRefused"
	case strings.Contains(msg, "550") || strings.Contains(msgLower, "relay"):
		return "smtpErrorRelay"
	case strings.Contains(msgLower, "eof"):
		return "smtpErrorEof"
	default:
		return "smtpErrorUnknown"
	}
}

func parseRecipients(toStr string) ([]string, error) {
	if strings.TrimSpace(toStr) == "" {
		return nil, nil
	}
	addresses, err := mail.ParseAddressList(toStr)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.Address)
	}
	return out, nil
}

// buildMessage assembles an RFC 5322 message. It emits the two mandatory
// header fields (Date, From) plus Message-ID, so strict receivers such as Gmail
// accept it and spam filters do not penalize a missing date or message id. The
// From header is a proper name-addr ("Name" <addr>) via net/mail, and a
// non-ASCII subject is RFC 2047 encoded.
// headerSanitizer drops CR/LF so a crafted address or name cannot inject extra
// header lines. Configured addresses are already validated at save time
// (entity.AllSetting.CheckValid), this is defense in depth for buildMessage.
var headerSanitizer = strings.NewReplacer("\r", "", "\n", "")

func resolveFrom(from, fromName string) (string, string) {
	parsed, err := mail.ParseAddress(from)
	if err != nil {
		return from, fromName
	}
	if fromName == "" {
		fromName = parsed.Name
	}
	return parsed.Address, fromName
}

func buildMessage(fromAddr, fromName string, to []string, subject, body string) []byte {
	fromAddr = headerSanitizer.Replace(fromAddr)
	fromName = headerSanitizer.Replace(fromName)
	from := (&mail.Address{Name: fromName, Address: fromAddr}).String()

	domain := "localhost"
	if at := strings.LastIndex(fromAddr, "@"); at >= 0 && at+1 < len(fromAddr) {
		domain = fromAddr[at+1:]
	}
	var token [16]byte
	_, _ = rand.Read(token[:])
	messageID := fmt.Sprintf("<%s@%s>", hex.EncodeToString(token[:]), domain)

	var msg strings.Builder
	fmt.Fprintf(&msg, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&msg, "Message-ID: %s\r\n", messageID)
	fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	return []byte(msg.String())
}
