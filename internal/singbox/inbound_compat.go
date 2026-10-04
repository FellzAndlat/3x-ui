package singbox

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func rejectXrayFields(options map[string]any, label string, keys ...string) error {
	for _, key := range keys {
		// Xray's server-side ML-DSA seed is an optional REALITY hardening
		// extension. sing-box REALITY has no equivalent field, but omitting it
		// does not change the base REALITY handshake and the source Xray config is
		// left untouched for switching back. Do not block an inbound solely for
		// this Xray-only extension; client-side verification remains strict.
		if key == "mldsa65Seed" && strings.HasPrefix(label, "inbound ") && strings.HasSuffix(label, " REALITY") {
			continue
		}
		if isMeaningfulCompatValue(options[key]) {
			return fmt.Errorf("%s: Xray option %s has no equivalent sing-box representation", label, key)
		}
	}
	return nil
}

func translateInboundCompatibility(out, raw map[string]any) error {
	tag, protocol := rawString(raw, "tag"), rawString(out, "type")
	settings, stream := rawObject(raw, "settings"), rawObject(raw, "streamSettings")
	label := fmt.Sprintf("inbound %q", tag)
	if protocol == "vless" {
		if value := rawString(settings, "decryption"); value != "" && value != "none" {
			return fmt.Errorf("%s: encrypted VLESS decryption cannot be represented by sing-box", label)
		}
		if err := rejectXrayFields(settings, label, "fallbacks"); err != nil {
			return err
		}
	}
	if protocol == "shadowsocks" {
		if network := rawString(settings, "network"); network != "" {
			switch network {
			case "tcp", "udp":
				out["network"] = network
			case "tcp,udp", "udp,tcp":
			default:
				return fmt.Errorf("%s has invalid Shadowsocks network %q", label, network)
			}
		}
	}
	if protocol == "mixed" || protocol == "socks" {
		if udp, present := settings["udp"].(bool); present && !udp {
			// These native inbounds have no option to disable SOCKS UDP independently.
			return fmt.Errorf("%s: sing-box %s cannot preserve udp=false", label, protocol)
		}
	}
	if protocol == "trojan" {
		if err := translateTrojanFallbacks(out, settings, label); err != nil {
			return err
		}
	}
	// ListenOptions share these socket controls with DialerOptions. Do not apply
	// outbound-only detour or DNS controls to a listener.
	sockopt := rawObject(stream, "sockopt")
	if err := rejectXrayFields(sockopt, label, "acceptProxyProtocol", "customSockopt", "tcpCongestion", "tcpMaxSeg", "tcpUserTimeout", "tcpWindowClamp", "tcpcongestion", "tproxy", "V6Only", "happyEyeballs", "dialerProxy"); err != nil {
		return err
	}
	listenStream := map[string]any{"sockopt": map[string]any{}}
	listenSockopt := listenStream["sockopt"].(map[string]any)
	for _, key := range []string{"interface", "mark", "tcpFastOpen", "tcpMptcp", "tcpKeepAliveIdle", "tcpKeepAliveInterval"} {
		if value, present := sockopt[key]; present {
			listenSockopt[key] = value
		}
	}
	if err := translateOutboundSockopt(out, listenStream, tag); err != nil {
		return err
	}
	if err := translateGRPCTransportCompatibility(out, stream, tag); err != nil {
		return err
	}
	return nil
}

func translateTrojanFallbacks(out, settings map[string]any, label string) error {
	fallbacks, ok := settings["fallbacks"].([]any)
	if settings["fallbacks"] != nil && !ok {
		return fmt.Errorf("%s has invalid fallbacks", label)
	}
	alpnFallbacks := map[string]any{}
	for _, raw := range fallbacks {
		fallback, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s has invalid fallback", label)
		}
		if err := rejectXrayFields(fallback, label+" fallback", "name", "path", "xver"); err != nil {
			return err
		}
		dest := fmt.Sprint(fallback["dest"])
		host, port, err := fallbackDestination(dest)
		if err != nil {
			return fmt.Errorf("%s fallback: %w", label, err)
		}
		target := map[string]any{"server": host, "server_port": port}
		alpn := rawString(fallback, "alpn")
		if alpn == "" {
			if out["fallback"] != nil {
				return fmt.Errorf("%s has multiple default fallbacks", label)
			}
			out["fallback"] = target
		} else {
			if alpnFallbacks[alpn] != nil {
				return fmt.Errorf("%s has duplicate fallback ALPN %q", label, alpn)
			}
			alpnFallbacks[alpn] = target
		}
	}
	if len(alpnFallbacks) > 0 {
		out["fallback_for_alpn"] = alpnFallbacks
	}
	return nil
}

func fallbackDestination(dest string) (string, int, error) {
	if port, err := strconv.Atoi(dest); err == nil {
		if port > 0 && port <= 65535 {
			return "127.0.0.1", port, nil
		}
		return "", 0, fmt.Errorf("invalid fallback port %q", dest)
	}
	host, portString, err := net.SplitHostPort(dest)
	if err != nil || host == "" {
		return "", 0, fmt.Errorf("unsupported fallback destination %q (expected TCP host:port)", dest)
	}
	port, err := strconv.Atoi(portString)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid fallback port %q", portString)
	}
	return host, port, nil
}

func translateInboundECHServerKeys(tlsOut, tlsIn map[string]any, label string) error {
	value := strings.TrimSpace(rawString(tlsIn, "echServerKeys"))
	if value == "" {
		return nil
	}

	var pemBytes []byte
	if block, rest := pem.Decode([]byte(value)); block != nil {
		if block.Type != "ECH KEYS" || len(strings.TrimSpace(string(rest))) != 0 || len(block.Bytes) == 0 {
			return fmt.Errorf("%s: invalid Xray echServerKeys PEM", label)
		}
		pemBytes = pem.EncodeToMemory(&pem.Block{Type: "ECH KEYS", Bytes: block.Bytes})
	} else {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(raw) == 0 {
			if err != nil {
				return fmt.Errorf("%s: invalid Xray echServerKeys: %w", label, err)
			}
			return fmt.Errorf("%s: invalid Xray echServerKeys: empty key set", label)
		}
		// Xray and sing-box use the same binary ECH key-set wire format. Xray
		// stores it as base64 in JSON; sing-box expects those bytes wrapped in an
		// ECH KEYS PEM block and exposed as a line array.
		pemBytes = pem.EncodeToMemory(&pem.Block{Type: "ECH KEYS", Bytes: raw})
	}
	if len(pemBytes) == 0 {
		return fmt.Errorf("%s: failed to encode Xray echServerKeys for sing-box", label)
	}
	lines := strings.Split(strings.TrimSpace(string(pemBytes)), "\n")
	tlsOut["ech"] = map[string]any{"enabled": true, "key": lines}
	return nil
}

func translateInboundTLSCompatibility(tlsOut, tlsIn map[string]any, tag string) error {
	label := fmt.Sprintf("inbound %q TLS", tag)
	if err := rejectXrayFields(tlsIn, label, "rejectUnknownSni", "echConfigList", "echSockopt", "verifyPeerCertByName", "pinnedPeerCertSha256"); err != nil {
		return err
	}
	if err := translateInboundECHServerKeys(tlsOut, tlsIn, label); err != nil {
		return err
	}
	for _, key := range []string{"minVersion", "maxVersion"} {
		if value := rawString(tlsIn, key); value != "" {
			native := "min_version"
			if key == "maxVersion" {
				native = "max_version"
			}
			tlsOut[native] = value
		}
	}
	if suites := rawString(tlsIn, "cipherSuites"); suites != "" {
		tlsOut["cipher_suites"] = strings.FieldsFunc(suites, func(r rune) bool { return r == ':' || r == ',' })
	}
	if curves := rawStrings(tlsIn, "curvePreferences"); len(curves) > 0 {
		for i, curve := range curves {
			curves[i] = normalizeXrayTLSCurve(curve)
		}
		tlsOut["curve_preferences"] = curves
	}
	certs, _ := tlsIn["certificates"].([]any)
	if len(certs) > 1 {
		return fmt.Errorf("%s: multiple Xray certificate identities require a native certificate provider", label)
	}
	for _, raw := range certs {
		cert, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s has an invalid certificate", label)
		}
		if usage := rawString(cert, "usage"); usage != "" && usage != "encipherment" {
			return fmt.Errorf("%s: certificate usage %q is not a server identity", label, usage)
		}
		if err := rejectXrayFields(cert, label, "ocspStapling", "oneTimeLoading", "buildChain"); err != nil {
			return err
		}
	}
	return nil
}

// TranslateXraySniffingRule uses modern route actions, not removed legacy
// inbound.sniff fields. Destination rewriting and exclusion lists have no
// equivalent action in sing-box; accepting them would change routing policy.
func TranslateXraySniffingRule(raw map[string]any) (map[string]any, error) {
	sniff := rawObject(raw, "sniffing")
	if !xrayBool(sniff, "enabled") {
		return nil, nil
	}
	label := fmt.Sprintf("inbound %q sniffing", rawString(raw, "tag"))
	if err := rejectXrayFields(sniff, label, "domainsExcluded", "metadataOnly"); err != nil {
		return nil, err
	}
	overrides := rawStrings(sniff, "destOverride")
	if len(overrides) > 0 && !xrayBool(sniff, "routeOnly") {
		return nil, fmt.Errorf("%s: destination override requires routeOnly=true for sing-box", label)
	}
	for i, protocol := range overrides {
		if protocol == "fakedns" || protocol == "fakedns+others" {
			return nil, fmt.Errorf("%s: FakeDNS sniffing cannot be represented", label)
		}
		if protocol == "https" {
			overrides[i] = "tls"
		}
	}
	rule := map[string]any{"inbound": []string{rawString(raw, "tag")}, "action": "sniff"}
	if len(overrides) > 0 {
		rule["sniffer"] = overrides
	}
	return rule, nil
}

func rawAccounts(settings map[string]any) ([]any, error) {
	accounts, ok := settings["accounts"].([]any)
	if settings["accounts"] != nil && !ok {
		return nil, fmt.Errorf("proxy inbound accounts must be an array")
	}
	result := make([]any, 0, len(accounts))
	for _, raw := range accounts {
		account, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("proxy inbound has an invalid account")
		}
		user, userOK := account["user"].(string)
		password, passwordOK := account["pass"].(string)
		if !userOK || !passwordOK {
			return nil, fmt.Errorf("proxy inbound account requires string user/pass credentials")
		}
		result = append(result, map[string]any{"email": user, "password": password})
	}
	return result, nil
}
