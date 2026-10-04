package email

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSendPlainReturnsOnStalledServer(t *testing.T) {
	orig := smtpDeadline
	smtpDeadline = 300 * time.Millisecond
	t.Cleanup(func() { smtpDeadline = orig })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	stall := make(chan struct{})
	defer close(stall)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-stall
	}()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), smtpDeadline)
		defer cancel()
		_, err := sendSMTP(ctx, smtpSettings{addr: ln.Addr().String(), host: "example.com", encryption: "none", from: "from@example.com", recipients: []string{"to@example.com"}}, []byte("body"))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a silent SMTP server, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sendPlain did not return on a stalled server within the deadline")
	}
}
