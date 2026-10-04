package ldaputil

import (
	"io"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestAuditLDAPIPv6URL(t *testing.T) {
	for _, host := range []string{"2001:db8::1", "[2001:db8::1]", "fe80::1%eth0"} {
		raw, err := serverURL(Config{Host: host, Port: 636, UseTLS: true})
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		addr, port, err := net.SplitHostPort(u.Host)
		if err != nil || port != "636" || addr == "" || u.Scheme != "ldaps" {
			t.Fatalf("invalid IPv6 URL: %s", raw)
		}
	}
}

func TestAuditLDAPSilentServerTimesOut(t *testing.T) {
	previous := ldapRequestTimeout
	ldapRequestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { ldapRequestTimeout = previous })
	for _, operation := range []string{"authenticate", "flags"} {
		t.Run(operation, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan struct{})
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
				close(done)
			}()
			host, rawPort, _ := net.SplitHostPort(ln.Addr().String())
			port, _ := strconv.Atoi(rawPort)
			cfg := Config{Host: host, Port: port, BindDN: "cn=test", Password: "test"}
			started := time.Now()
			if operation == "authenticate" {
				_, err = AuthenticateUser(cfg, "user", "password")
			} else {
				_, err = FetchVlessFlags(cfg)
			}
			if err == nil || time.Since(started) > time.Second {
				t.Fatalf("silent LDAP server not bounded: %v", err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("LDAP timeout leaked its connection")
			}
		})
	}
}
