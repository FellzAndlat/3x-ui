package ldaputil

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type Config struct {
	Host               string
	Port               int
	UseTLS             bool
	InsecureSkipVerify bool
	BindDN             string
	Password           string
	BaseDN             string
	UserFilter         string
	UserAttr           string
	FlagField          string
	TruthyVals         []string
	Invert             bool
}

func tlsConfig(cfg Config) *tls.Config {
	return &tls.Config{ServerName: strings.Trim(cfg.Host, "[]"), InsecureSkipVerify: cfg.InsecureSkipVerify}
}

const ldapConnectTimeout = 5 * time.Second

var ldapRequestTimeout = 10 * time.Second

func serverURL(cfg Config) (string, error) {
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return "", fmt.Errorf("invalid LDAP host or port")
	}
	scheme := "ldap"
	if cfg.UseTLS {
		scheme = "ldaps"
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(cfg.Host, "[]"), strconv.Itoa(cfg.Port))}
	return u.String(), nil
}

func dialServer(cfg Config) (*ldap.Conn, error) {
	endpoint, err := serverURL(cfg)
	if err != nil {
		return nil, err
	}
	opts := []ldap.DialOpt{ldap.DialWithDialer(&net.Dialer{Timeout: ldapConnectTimeout})}
	if cfg.UseTLS {
		opts = append(opts, ldap.DialWithTLSConfig(tlsConfig(cfg)))
	}
	conn, err := ldap.DialURL(endpoint, opts...)
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(ldapRequestTimeout)
	return conn, nil
}

func searchTimeLimit() int {
	return max(1, int((ldapRequestTimeout+time.Second-1)/time.Second))
}

// FetchVlessFlags returns map[email]enabled
func FetchVlessFlags(cfg Config) (map[string]bool, error) {
	conn, err := dialServer(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.Password); err != nil {
			return nil, err
		}
	}

	if cfg.UserFilter == "" {
		cfg.UserFilter = "(objectClass=person)"
	}
	if cfg.UserAttr == "" {
		cfg.UserAttr = "mail"
	}
	// if field not set we fallback to legacy vless_enabled
	if cfg.FlagField == "" {
		cfg.FlagField = "vless_enabled"
	}

	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, searchTimeLimit(), false,
		cfg.UserFilter,
		[]string{cfg.UserAttr, cfg.FlagField},
		nil,
	)

	res, err := conn.Search(req)
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool, len(res.Entries))
	for _, e := range res.Entries {
		user := e.GetAttributeValue(cfg.UserAttr)
		if user == "" {
			continue
		}
		val := e.GetAttributeValue(cfg.FlagField)
		enabled := slices.Contains(cfg.TruthyVals, val)
		if cfg.Invert {
			enabled = !enabled
		}
		result[user] = enabled
	}
	return result, nil
}

// AuthenticateUser searches user by cfg.UserAttr and attempts to bind with provided password.
func AuthenticateUser(cfg Config, username, password string) (bool, error) {
	conn, err := dialServer(cfg)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	// Optional initial bind for search
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.Password); err != nil {
			return false, err
		}
	}

	if cfg.UserFilter == "" {
		cfg.UserFilter = "(objectClass=person)"
	}
	if cfg.UserAttr == "" {
		cfg.UserAttr = "uid"
	}

	// Build filter to find specific user
	filter := fmt.Sprintf("(&%s(%s=%s))", cfg.UserFilter, cfg.UserAttr, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, searchTimeLimit(), false,
		filter,
		[]string{"dn"},
		nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return false, err
	}
	if len(res.Entries) == 0 {
		return false, nil
	}
	userDN := res.Entries[0].DN
	// Try to bind as the user
	if err := conn.Bind(userDN, password); err != nil {
		return false, nil
	}
	return true, nil
}
