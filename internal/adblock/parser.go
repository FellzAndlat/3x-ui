package adblock

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strings"
)

const maxScanTokenSize = 1024 * 1024

// ParseDomains reads a hosts-style or plain-domain block list and returns a
// normalized, deterministic set of domains. Entries present in allowlist are
// removed from the result. Hosts/plain names match exactly; ||host^ and
// domain:host match the name and all its subdomains. Allowlist names cover
// subdomains. Broad suffix rules overlapping an allowed child are omitted.
//
// Supported input examples:
//
//	0.0.0.0 ads.example.com
//	127.0.0.1 tracker.example.net # comment
//	ads.example.org
//	||telemetry.example.com^
func ParseDomains(r io.Reader, allowlist []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, entry := range allowlist {
		if domain, ok := normalizeDomain(entry); ok {
			allowed[domain] = struct{}{}
		}
	}

	blocked := make(map[string]struct{})
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxScanTokenSize)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		// Only unconditional, domain-only ABP exceptions are enforceable.
		if strings.HasPrefix(line, "@@||") && strings.HasSuffix(line, "^") {
			if name, ok := normalizeDomain(strings.TrimSuffix(strings.TrimPrefix(line, "@@||"), "^")); ok {
				allowed[name] = struct{}{}
			}
			continue
		}
		for _, entry := range domainsFromLine(line) {
			blocked[entry] = struct{}{}
		}
		if len(blocked) > DefaultMaxDomains {
			return nil, fmt.Errorf("blocklist exceeds %d domains", DefaultMaxDomains)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan adblock list: %w", err)
	}
	// Build ancestor membership once; filtering stays linear in the number
	// of labels even for large allowlists.
	allowedAncestors := make(map[string]struct{})
	for name := range allowed {
		for parent := name; parent != ""; parent = parentDomain(parent) {
			allowedAncestors[parent] = struct{}{}
		}
	}
	for entry := range blocked {
		name := strings.TrimPrefix(entry, "domain:")
		exempt := false
		for parent := name; parent != ""; parent = parentDomain(parent) {
			if _, ok := allowed[parent]; ok {
				exempt = true
				break
			}
		}
		// Xray suffix matchers cannot contain holes. Omit a broad rule
		// covering an allowed child instead of forcing direct routing.
		if strings.HasPrefix(entry, "domain:") {
			if _, ok := allowedAncestors[name]; ok {
				exempt = true
			}
		}
		if exempt {
			delete(blocked, entry)
		}
	}
	result := make([]string, 0, len(blocked))
	for domain := range blocked {
		result = append(result, domain)
	}
	sort.Strings(result)
	return result, nil
}

func domainsFromLine(line string) []string {
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") {
		return nil
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if strings.HasPrefix(line, "||") && strings.HasSuffix(line, "^") {
		if domain, ok := normalizeDomain(strings.TrimSuffix(strings.TrimPrefix(line, "||"), "^")); ok {
			return []string{"domain:" + domain}
		}
		return nil
	}
	if strings.ContainsAny(line, "*/$|@") {
		return nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	if addr, err := netip.ParseAddr(strings.Trim(fields[0], "[]")); err == nil {
		// Normal-address hosts mappings are redirects, not block entries.
		addr = addr.Unmap()
		if !addr.IsUnspecified() && !addr.IsLoopback() {
			return nil
		}
		var result []string
		for _, alias := range fields[1:] {
			if domain, ok := normalizeDomain(alias); ok {
				result = append(result, domain)
			}
		}
		return result
	}
	if len(fields) != 1 {
		return nil
	}
	if domain, ok := normalizeDomain(fields[0]); ok {
		if strings.HasPrefix(strings.ToLower(fields[0]), "domain:") {
			domain = "domain:" + domain
		}
		return []string{domain}
	}
	return nil
}

func normalizeDomain(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "domain:")
	value = strings.TrimPrefix(value, "full:")
	value = strings.TrimSuffix(value, ".")
	if _, err := netip.ParseAddr(value); err == nil {
		return "", false
	}
	if value == "" || len(value) > 253 || value == "localhost" {
		return "", false
	}

	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, r := range label {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				continue
			}
			return "", false
		}
	}
	return value, true
}

func parentDomain(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
}
