package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/xray/geodata"
)

// TranslateXrayRouting provides a compile-safe compatibility layer for the
// panel's Xray routing configuration. Unsupported legacy matchers are rejected
// instead of being emitted as invalid sing-box fields.
func TranslateXrayRouting(raw map[string]any) (map[string]any, error) {
	return TranslateXrayRoutingWithGeoData(raw, nil)
}

// TranslateXrayRoutingWithGeoData expands the categories in Xray's .dat files
// into sing-box matchers. Never omit a failed category: doing so could send
// traffic through a different outbound than the administrator configured.
func TranslateXrayRoutingWithGeoData(raw map[string]any, store *geodata.Store) (map[string]any, error) {
	out := map[string]any{}
	rulesRaw, ok := raw["rules"].([]any)
	if raw["rules"] != nil && !ok {
		return nil, fmt.Errorf("routing rules has invalid configuration")
	}
	strategy := compatString(raw["domainStrategy"])
	if strategy == "IPIfNonMatch" {
		return nil, fmt.Errorf("routing IPIfNonMatch requires two-pass routing; use an explicit native sing-box route")
	}
	if strategy != "" && strategy != "AsIs" && strategy != "IPOnDemand" && TranslateXrayDomainStrategy(strategy) == "" {
		return nil, fmt.Errorf("unsupported Xray routing domainStrategy %q", strategy)
	}
	rules := make([]map[string]any, 0, len(rulesRaw))
	for i, item := range rulesRaw {
		xr, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("routing rule %d has invalid configuration", i)
		}
		r := map[string]any{}
		for _, key := range []string{"attrs", "vlessRoute", "localIP", "localPort", "process", "localOS", "webhook"} {
			value := xr[key]
			present := value != nil
			switch v := value.(type) {
			case string:
				present = strings.TrimSpace(v) != ""
			case []any:
				present = len(v) > 0
			case map[string]any:
				present = len(v) > 0
			}
			if present {
				return nil, fmt.Errorf("routing rule %d: unsupported matcher %s", i, key)
			}
		}
		inboundTags := compatStringSlice(xr["inboundTag"])
		if len(inboundTags) > 0 {
			r["inbound"] = inboundTags
		}
		domains := compatStringSlice(xr["domain"])
		if xr["domain"] == nil {
			domains = compatStringSlice(xr["domains"])
		}
		if len(domains) > 0 {
			var expanded []string
			for _, domain := range domains {
				if isGeoToken(domain, "geosite:") || isGeoToken(domain, "ext:") {
					entries, err := routingGeoEntries(store, domain, geodata.KindSite)
					if err != nil {
						return nil, fmt.Errorf("routing rule %d: domain %q: %w", i, domain, err)
					}
					for _, entry := range entries {
						switch entry.Kind {
						case "domain":
							expanded = append(expanded, "domain:"+entry.Value)
						case "full":
							expanded = append(expanded, "full:"+entry.Value)
						case "keyword":
							expanded = append(expanded, "keyword:"+entry.Value)
						case "regexp":
							expanded = append(expanded, "regexp:"+entry.Value)
						default:
							return nil, fmt.Errorf("routing rule %d: domain %q has invalid entry %q", i, domain, entry.Value)
						}
					}
				} else {
					expanded = append(expanded, domain)
				}
			}
			domains = expanded
			if err := translateCompatDomains(r, domains); err != nil {
				return nil, fmt.Errorf("routing rule %d: %w", i, err)
			}
		}
		if ips := compatStringSlice(xr["ip"]); len(ips) > 0 {
			cidrs := make([]string, 0, len(ips))
			for _, ip := range ips {
				if !strings.EqualFold(ip, "geoip:private") && (isGeoToken(ip, "geoip:") || isGeoToken(ip, "ext:")) {
					entries, err := routingGeoEntries(store, ip, geodata.KindIP)
					if err != nil {
						return nil, fmt.Errorf("routing rule %d: IP %q: %w", i, ip, err)
					}
					for _, entry := range entries {
						cidrs = append(cidrs, entry.Value)
					}
					continue
				}
				if strings.EqualFold(ip, "geoip:private") {
					// Xray's geoip:private is a semantic matcher for private
					// addresses. sing-box has the native equivalent.
					r["ip_is_private"] = true
					continue
				}
				if net.ParseIP(ip) == nil {
					if _, _, err := net.ParseCIDR(ip); err != nil {
						return nil, fmt.Errorf("routing rule %d: unsupported IP matcher %q", i, ip)
					}
				}
				cidrs = append(cidrs, ip)
			}
			if len(cidrs) > 0 {
				r["ip_cidr"] = cidrs
			}
		}
		for _, field := range []struct{ xray, singbox string }{{"port", "port"}, {"sourcePort", "source_port"}} {
			if err := translateCompatPorts(r, field.singbox, xr[field.xray]); err != nil {
				return nil, fmt.Errorf("routing rule %d: %s: %w", i, field.xray, err)
			}
		}
		sources := compatStringSlice(xr["sourceIP"])
		if xr["sourceIP"] == nil {
			sources = compatStringSlice(xr["source"])
		}
		var sourceCIDRs []string
		for _, source := range sources {
			if !strings.EqualFold(source, "geoip:private") && (isGeoToken(source, "geoip:") || isGeoToken(source, "ext:")) {
				entries, err := routingGeoEntries(store, source, geodata.KindIP)
				if err != nil {
					return nil, fmt.Errorf("routing rule %d: sourceIP %q: %w", i, source, err)
				}
				for _, entry := range entries {
					sourceCIDRs = append(sourceCIDRs, entry.Value)
				}
				continue
			}
			if strings.EqualFold(source, "geoip:private") {
				r["source_ip_is_private"] = true
				continue
			}
			if net.ParseIP(source) == nil {
				if _, _, err := net.ParseCIDR(source); err != nil {
					return nil, fmt.Errorf("routing rule %d: unsupported sourceIP matcher %q", i, source)
				}
			}
			sourceCIDRs = append(sourceCIDRs, source)
		}
		if len(sourceCIDRs) > 0 {
			r["source_ip_cidr"] = sourceCIDRs
		}
		if network := compatString(xr["network"]); network != "" {
			parts := strings.Split(network, ",")
			validated := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(strings.ToLower(part))
				if part == "" {
					continue
				}
				if part != "tcp" && part != "udp" {
					return nil, fmt.Errorf("routing rule %d: unsupported network %q", i, network)
				}
				validated = append(validated, part)
			}
			switch len(validated) {
			case 0:
				return nil, fmt.Errorf("routing rule %d: unsupported network %q", i, network)
			case 1:
				r["network"] = validated[0]
			default:
				r["network"] = validated
			}
		}
		if users := compatStringSlice(xr["user"]); len(users) > 0 {
			r["auth_user"] = users
		}
		if protocols := compatStringSlice(xr["protocol"]); len(protocols) > 0 {
			r["protocol"] = protocols
		}
		if strategy == "IPOnDemand" && (r["ip_cidr"] != nil || r["ip_is_private"] != nil) {
			resolve := map[string]any{"action": "resolve"}
			for key, value := range r {
				if key != "ip_cidr" && key != "ip_is_private" {
					resolve[key] = value
				}
			}
			rules = append(rules, resolve)
		}
		// Xray combines domain and IP constraints with AND; sing-box groups
		// destination matchers with OR unless they are separate logical rules.
		if len(domains) > 0 && (r["ip_cidr"] != nil || r["ip_is_private"] != nil) {
			ipRule := map[string]any{}
			for _, key := range []string{"ip_cidr", "ip_is_private"} {
				if value, ok := r[key]; ok {
					ipRule[key] = value
					delete(r, key)
				}
			}
			r = map[string]any{"type": "logical", "mode": "and", "rules": []map[string]any{r, ipRule}}
		}
		if outbound := compatString(xr["outboundTag"]); outbound != "" {
			// The stock Xray template contains an internal "api" inbound/rule used
			// only by Xray's own API. sing-box has no matching Xray API outbound,
			// so carrying the rule over references a nonexistent outbound and makes
			// sing-box reject the complete configuration.
			if outbound == "api" && containsCompatString(inboundTags, "api") {
				if len(inboundTags) != 1 {
					return nil, fmt.Errorf("routing rule %d mixes Xray API and user inbounds", i)
				}
				continue
			}
			r["action"] = "route"
			r["outbound"] = outbound
		} else if balancer := compatString(xr["balancerTag"]); balancer != "" {
			// The compatibility layer materializes Xray balancers as sing-box
			// selector outbounds with the same tag.
			r["action"] = "route"
			r["outbound"] = balancer
		} else {
			return nil, fmt.Errorf("routing rule %d has no outboundTag", i)
		}
		rules = append(rules, r)
	}
	if len(rules) > 0 {
		out["rules"] = rules
	}
	return out, nil
}

const maxRoutingGeoEntries = 500000

func isGeoToken(value, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), prefix)
}

func routingGeoEntries(store *geodata.Store, token string, kind geodata.GeoKind) ([]geodata.GeoEntry, error) {
	if store == nil {
		return nil, fmt.Errorf("geodata directory is unavailable")
	}
	token = strings.TrimSpace(token)
	var file, category string
	if isGeoToken(token, "ext:") {
		file, category, _ = strings.Cut(token[4:], ":")
	} else if kind == geodata.KindIP && isGeoToken(token, "geoip:") {
		file, category = "geoip.dat", token[6:]
	} else if kind == geodata.KindSite && isGeoToken(token, "geosite:") {
		file, category = "geosite.dat", token[8:]
	}
	if file == "" || category == "" || strings.ContainsAny(category, "@!:") {
		return nil, fmt.Errorf("unsupported geodata category")
	}
	entries, err := store.AllEntries(file, category, maxRoutingGeoEntries)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("geodata category is empty")
	}
	for _, entry := range entries {
		if kind == geodata.KindIP && entry.Kind != "cidr" || kind == geodata.KindSite && entry.Kind == "cidr" {
			return nil, fmt.Errorf("geodata file %q has the wrong type", file)
		}
	}
	return entries, nil
}

func translateCompatPorts(dst map[string]any, field string, value any) error {
	if value == nil {
		return nil
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case float64:
		text = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		text = strconv.Itoa(v)
	case json.Number:
		text = v.String()
	default:
		return fmt.Errorf("invalid port value %v", value)
	}
	var ports []uint16
	var ranges []string
	for part := range strings.SplitSeq(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		start, err := strconv.ParseUint(strings.TrimSpace(lo), 10, 16)
		if err != nil {
			return fmt.Errorf("invalid port %q", part)
		}
		if !isRange {
			ports = append(ports, uint16(start))
			continue
		}
		end, err := strconv.ParseUint(strings.TrimSpace(hi), 10, 16)
		if err != nil || end < start {
			return fmt.Errorf("invalid port range %q", part)
		}
		ranges = append(ranges, fmt.Sprintf("%d:%d", start, end))
	}
	if len(ports) > 0 {
		dst[field] = ports
	}
	if len(ranges) > 0 {
		dst[field+"_range"] = ranges
	}
	return nil
}

func TranslateXrayDomainStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "UseIPv4", "UseIPv4v6":
		return "prefer_ipv4"
	case "UseIPv6", "UseIPv6v4":
		return "prefer_ipv6"
	case "ForceIPv4":
		return "ipv4_only"
	case "ForceIPv6":
		return "ipv6_only"
	default:
		return ""
	}
}

// TranslateXrayBalancers converts the simple selector portion of Xray
// balancers into native sing-box selector outbounds. Health/least-load
// strategies are not directly equivalent, so the first configured selector
// is used as the deterministic default unless Xray's fallbackTag is itself
// one of the selected outbounds.
func TranslateXrayBalancers(raw map[string]any, available ...map[string]any) ([]map[string]any, error) {
	items, ok := raw["balancers"].([]any)
	if raw["balancers"] != nil && !ok {
		return nil, fmt.Errorf("routing balancers has invalid configuration")
	}
	if len(items) == 0 {
		return nil, nil
	}
	result := make([]map[string]any, 0, len(items))
	for i, item := range items {
		balancer, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("balancer %d has invalid configuration", i)
		}
		tag := compatString(balancer["tag"])
		if tag == "" {
			return nil, fmt.Errorf("balancer %d has an empty tag", i)
		}
		selectors := compatStringSlice(balancer["selector"])
		if len(selectors) == 0 {
			return nil, fmt.Errorf("balancer %q has no selectors", tag)
		}
		if len(available) > 0 {
			var expanded []string
			for _, outbound := range available {
				target := rawString(outbound, "tag")
				if target == "" || rawString(outbound, "protocol") == "dns" {
					continue
				}
				for _, prefix := range selectors {
					if strings.HasPrefix(target, prefix) {
						expanded = append(expanded, target)
						break
					}
				}
			}
			selectors = expanded
		}
		if len(selectors) == 0 {
			return nil, fmt.Errorf("balancer %q selectors match no outbound", tag)
		}
		strategy := rawString(rawObject(balancer, "strategy"), "type")
		if len(selectors) > 1 || strategy != "" && strategy != "random" {
			return nil, fmt.Errorf("balancer %q strategy %q cannot be preserved by a static sing-box selector; configure a native selector/urltest", tag, strategy)
		}
		defaultTag := selectors[0]
		fallback := compatString(balancer["fallbackTag"])
		if fallback != "" && !containsCompatString(selectors, fallback) {
			return nil, fmt.Errorf("balancer %q fallback %q cannot be preserved by a static selector", tag, fallback)
		}
		if containsCompatString(selectors, fallback) {
			defaultTag = fallback
		}
		result = append(result, map[string]any{
			"type":      "selector",
			"tag":       tag,
			"outbounds": selectors,
			"default":   defaultTag,
		})
	}
	return result, nil
}

func RewriteDNSOutboundRoutes(route map[string]any, dnsTags map[string]struct{}) {
	if route == nil || len(dnsTags) == 0 {
		return
	}
	if final := compatString(route["final"]); final != "" {
		if _, ok := dnsTags[final]; ok {
			route["final"] = "direct"
			rule := map[string]any{"action": "hijack-dns"}
			rules, _ := route["rules"].([]map[string]any)
			rules = append(rules, rule)
			route["rules"] = rules
		}
	}
	rules, _ := route["rules"].([]map[string]any)
	for _, rule := range rules {
		outbound := compatString(rule["outbound"])
		if outbound == "" {
			continue
		}
		if _, ok := dnsTags[outbound]; ok {
			delete(rule, "outbound")
			rule["action"] = "hijack-dns"
		}
	}
}

func containsCompatString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func translateCompatDomains(dst map[string]any, domains []string) error {
	var exact, suffix, keyword, regex []string
	for _, value := range domains {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "!") || strings.HasPrefix(value, "geosite:") || strings.HasPrefix(value, "ext:") || strings.HasPrefix(value, "dotless:") {
			return fmt.Errorf("unsupported domain matcher %q", value)
		}
		switch {
		case strings.HasPrefix(value, "regexp:"):
			regex = append(regex, strings.TrimPrefix(value, "regexp:"))
		case strings.HasPrefix(value, "domain:"):
			suffix = append(suffix, strings.TrimPrefix(value, "domain:"))
		case strings.HasPrefix(value, "keyword:"):
			keyword = append(keyword, strings.TrimPrefix(value, "keyword:"))
		case strings.HasPrefix(value, "full:"):
			exact = append(exact, strings.TrimPrefix(value, "full:"))
		default:
			keyword = append(keyword, value)
		}
	}
	if len(exact) > 0 {
		dst["domain"] = exact
	}
	if len(suffix) > 0 {
		dst["domain_suffix"] = suffix
	}
	if len(keyword) > 0 {
		dst["domain_keyword"] = keyword
	}
	if len(regex) > 0 {
		dst["domain_regex"] = regex
	}
	return nil
}

// TranslateXrayDNS converts the common Xray DNS server forms used by the
// panel to the generic sing-box DNS object without depending on sing-box
// internal Go packages.
func TranslateXrayDNS(raw map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, field := range []string{"disableFallback", "disableFallbackIfMatch", "enableParallelQuery", "serveStale", "serveExpiredTTL"} {
		if isMeaningfulCompatValue(raw[field]) {
			return nil, fmt.Errorf("DNS %s cannot be translated to sing-box", field)
		}
	}
	if isMeaningfulCompatValue(raw["useSystemHosts"]) {
		return nil, fmt.Errorf("DNS useSystemHosts cannot be translated to sing-box")
	}
	if strategy := compatString(raw["queryStrategy"]); strategy != "" {
		switch strings.ToLower(strategy) {
		case "useip", "use_ip", "use-ip":
		case "useip4", "useipv4", "use_ip4", "use_ipv4":
			out["strategy"] = "ipv4_only"
		case "useip6", "useipv6", "use_ip6", "use_ipv6":
			out["strategy"] = "ipv6_only"
		default:
			return nil, fmt.Errorf("DNS queryStrategy %q cannot be translated to sing-box", strategy)
		}
	}
	if disableCache, ok := raw["disableCache"].(bool); ok && disableCache {
		out["disable_cache"] = true
	}
	serversRaw, ok := raw["servers"].([]any)
	if raw["servers"] != nil && !ok {
		return nil, fmt.Errorf("DNS servers has invalid configuration")
	}
	servers := make([]map[string]any, 0, len(serversRaw))
	needsBootstrap := false
	timeoutMillis := uint64(4000)
	for i, item := range serversRaw {
		var address string
		var extra map[string]any
		switch value := item.(type) {
		case string:
			address = strings.TrimSpace(value)
		case map[string]any:
			extra = value
			address = strings.TrimSpace(compatString(value["address"]))
			for _, field := range []string{"domains", "expectedIPs", "expectIPs", "unexpectedIPs", "clientIp", "skipFallback", "finalQuery", "serveStale", "serveExpiredTTL", "disableCache"} {
				if isMeaningfulCompatValue(value[field]) {
					return nil, fmt.Errorf("DNS server %d %s cannot be translated to sing-box", i, field)
				}
			}
			if strategy := compatString(value["queryStrategy"]); strategy != "" && !strings.EqualFold(strategy, "UseIP") {
				return nil, fmt.Errorf("DNS server %d queryStrategy cannot be translated to sing-box", i)
			}
		default:
			return nil, fmt.Errorf("DNS server %d has an invalid configuration", i)
		}
		serverTimeout := uint64(4000)
		if extra["timeoutMs"] != nil {
			switch extra["timeoutMs"].(type) {
			case float64, int, json.Number:
			default:
				return nil, fmt.Errorf("DNS server %d has invalid timeoutMs", i)
			}
			value, err := strconv.ParseUint(fmt.Sprint(extra["timeoutMs"]), 10, 64)
			if err != nil || value > uint64((1<<63-1)/1000000) {
				return nil, fmt.Errorf("DNS server %d has invalid timeoutMs", i)
			}
			if value != 0 {
				serverTimeout = value
			}
		}
		if i > 0 && serverTimeout != timeoutMillis {
			return nil, fmt.Errorf("DNS servers have different timeouts")
		}
		timeoutMillis = serverTimeout
		if address == "" {
			return nil, fmt.Errorf("DNS server %d has an empty address", i)
		}
		if net.ParseIP(address) != nil && strings.Contains(address, ":") {
			address = "[" + address + "]"
		}
		server := map[string]any{"tag": fmt.Sprintf("dns-%d", i+1)}
		if strings.EqualFold(address, "localhost") || strings.EqualFold(address, "local") {
			server["type"] = "local"
		} else {
			if strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "h3://") {
				server["type"] = "https"
				if strings.HasPrefix(address, "h3://") {
					server["type"] = "h3"
				}
				if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
					server["server"] = u.Hostname()
					if u.EscapedPath() != "" {
						server["path"] = u.EscapedPath()
						if u.RawQuery != "" {
							server["path"] = u.EscapedPath() + "?" + u.RawQuery
						}
					}
				} else {
					return nil, fmt.Errorf("DNS server %d has invalid address %q", i, address)
				}
			} else if strings.HasPrefix(address, "tls://") {
				server["type"] = "tls"
				if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
					server["server"] = u.Hostname()
				} else {
					return nil, fmt.Errorf("DNS server %d has invalid TLS address %q", i, address)
				}
			} else if strings.HasPrefix(address, "quic://") {
				server["type"] = "quic"
				if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
					server["server"] = u.Hostname()
				} else {
					return nil, fmt.Errorf("DNS server %d has invalid QUIC address %q", i, address)
				}
			} else if strings.HasPrefix(address, "tcp://") {
				server["type"] = "tcp"
				u, err := url.Parse(address)
				if err != nil || u.Hostname() == "" {
					return nil, fmt.Errorf("DNS server %d has invalid TCP address %q", i, address)
				}
				server["server"] = u.Hostname()
			} else if strings.HasPrefix(address, "udp://") {
				server["type"] = "udp"
				if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
					server["server"] = u.Hostname()
				} else {
					return nil, fmt.Errorf("DNS server %d has invalid UDP address %q", i, address)
				}
			} else if strings.Contains(address, "://") {
				return nil, fmt.Errorf("DNS server %d uses unsupported address %q", i, address)
			} else {
				server["type"] = "udp"
				if u, err := url.Parse("udp://" + address); err == nil && u.Hostname() != "" {
					server["server"] = u.Hostname()
				} else {
					return nil, fmt.Errorf("DNS server %d has invalid UDP address %q", i, address)
				}
			}
		}
		if server["type"] != "local" {
			u, err := url.Parse(address)
			if err != nil || !strings.Contains(address, "://") {
				u, err = url.Parse("udp://" + address)
			}
			if err != nil {
				return nil, fmt.Errorf("DNS server %d has invalid address %q: %w", i, address, err)
			}
			defaultPort := 53
			switch server["type"] {
			case "https", "h3":
				defaultPort = 443
			case "tls", "quic":
				defaultPort = 853
			}
			port, err := compatDNSPort(u.Port(), defaultPort)
			if err != nil {
				return nil, fmt.Errorf("DNS server %d: %w", i, err)
			}
			if extra["port"] != nil {
				explicit, err := compatDNSPort(fmt.Sprint(extra["port"]), port)
				if err != nil {
					return nil, fmt.Errorf("DNS server %d: %w", i, err)
				}
				if u.Port() != "" && explicit != port {
					return nil, fmt.Errorf("DNS server %d has conflicting ports", i)
				}
				port = explicit
			}
			server["server_port"] = port
			if net.ParseIP(compatString(server["server"])) == nil {
				server["domain_resolver"] = "panel-bootstrap"
				needsBootstrap = true
			}
		}
		servers = append(servers, server)
	}
	if len(servers) > 1 {
		var fallbackRules []map[string]any
		for _, server := range servers {
			fallbackRules = append(fallbackRules, map[string]any{"action": "evaluate", "server": server["tag"], "tag": server["tag"]})
		}
		for _, server := range servers {
			fallbackRules = append(fallbackRules, map[string]any{"match_response": server["tag"], "response_rcode": "NOERROR", "action": "respond"})
		}
		out["rules"] = fallbackRules
	}
	if needsBootstrap {
		servers = append(servers, map[string]any{"type": "local", "tag": "panel-bootstrap"})
	}
	if raw["hosts"] != nil {
		if _, ok := raw["hosts"].(map[string]any); !ok {
			return nil, fmt.Errorf("DNS hosts has invalid configuration")
		}
	}
	if hosts, ok := raw["hosts"].(map[string]any); ok && len(hosts) > 0 {
		predefined := make(map[string]any, len(hosts))
		for domain, record := range hosts {
			if strings.TrimSpace(domain) == "" || strings.ContainsAny(domain, ":/* ") {
				return nil, fmt.Errorf("DNS host %q cannot be translated to sing-box", domain)
			}
			addresses := compatStringSlice(record)
			if len(addresses) == 0 {
				return nil, fmt.Errorf("DNS host %q has no IP addresses", domain)
			}
			for _, address := range addresses {
				if net.ParseIP(address) == nil {
					return nil, fmt.Errorf("DNS host %q has unsupported address %q", domain, address)
				}
			}
			predefined[domain] = addresses
		}
		servers = append(servers, map[string]any{"type": "hosts", "tag": "panel-hosts", "predefined": predefined})
		existing, _ := out["rules"].([]map[string]any)
		out["rules"] = append([]map[string]any{{"preferred_by": "panel-hosts", "action": "route", "server": "panel-hosts"}}, existing...)
	}
	if len(servers) == 0 || len(servers) == 1 && servers[0]["type"] == "hosts" {
		servers = append(servers, map[string]any{"type": "local", "tag": "panel-default"})
	}
	if len(servers) > 0 {
		out["servers"] = servers
		out["timeout"] = fmt.Sprintf("%dms", timeoutMillis)
		for _, server := range servers {
			if server["type"] != "hosts" {
				out["final"] = server["tag"]
				break
			}
		}
	}
	if clientIP := compatString(raw["clientIp"]); clientIP != "" {
		if net.ParseIP(clientIP) == nil {
			return nil, fmt.Errorf("DNS has invalid clientIp %q", clientIP)
		}
		out["client_subnet"] = clientIP
	}
	return out, nil
}

func isMeaningfulCompatValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case int:
		return v != 0
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		for _, item := range v {
			if isMeaningfulCompatValue(item) {
				return true
			}
		}
		return false
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if isMeaningfulCompatValue(item) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func compatString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func compatStringSlice(v any) []string {
	switch values := v.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := compatString(value); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return values
	case string:
		if s := strings.TrimSpace(values); s != "" {
			return []string{s}
		}
	}
	return nil
}

func compatDNSPort(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("invalid DNS port %q", value)
	}
	return int(port), nil
}

// ValidateXrayDNSRouting rejects virtual DNS inbound policies that sing-box's
// DNS clients cannot reproduce. An unused DNS tag is harmless metadata.
func ValidateXrayDNSRouting(dns, routing map[string]any) error {
	tags := map[string]bool{}
	if tag := rawString(dns, "tag"); tag != "" {
		tags[tag] = true
	}
	servers, _ := dns["servers"].([]any)
	for _, server := range servers {
		if server, ok := server.(map[string]any); ok {
			if tag := rawString(server, "tag"); tag != "" {
				tags[tag] = true
			}
		}
	}
	rules, _ := routing["rules"].([]any)
	for i, item := range rules {
		rule, _ := item.(map[string]any)
		for _, tag := range compatStringSlice(rule["inboundTag"]) {
			if tags[tag] {
				return fmt.Errorf("DNS virtual inbound tag %q used by routing rule %d cannot be translated to sing-box", tag, i+1)
			}
		}
	}
	return nil
}
