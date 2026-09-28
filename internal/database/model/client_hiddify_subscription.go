package model

import (
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// normalizeHiddifySubURI keeps imported Hiddify subscription bases on the
// public HTTPS vhost. Older databases may still contain the regular
// subscription listener port (for example :2096); imported Hiddify links must
// instead use standard HTTPS port 443.
func normalizeHiddifySubURI(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return raw
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return raw
	}

	host := parsed.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	return "https://" + host + path
}

// AfterFind normalizes legacy Hiddify URLs as records leave the database. This
// covers existing installations without coupling imported users to the current
// regular subscription port and keeps every consumer (UI, Happ, Telegram and
// exports) on the same standard-443 URL.
func (r *ClientRecord) AfterFind(_ *gorm.DB) error {
	if r.HiddifySubURI != "" {
		r.HiddifySubURI = normalizeHiddifySubURI(r.HiddifySubURI)
	}
	return nil
}
