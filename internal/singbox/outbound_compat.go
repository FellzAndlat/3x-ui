package singbox

import (
	"fmt"
	"maps"
	"net"
	"strconv"
	"strings"
)

func xrayBool(m map[string]any, key string) bool {
	value, _ := m[key].(bool)
	return value
}

func xrayEnabled(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v > 0
	case int64:
		return v > 0
	case float64:
		return v > 0
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "true") {
			return true
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return err == nil && n > 0
	default:
		return false
	}
}

func xrayUoTVersion(server map[string]any) int {
	version := rawInt(server, "uotVersion")
	if version == 0 {
		version = rawInt(server, "UoTVersion")
	}
	if version == 0 {
		version = 1
	}
	return version
}

func translateHTTPOutboundOptions(out map[string]any, settings map[string]any) {
	if headers, ok := settings["headers"].(map[string]any); ok && len(headers) > 0 {
		out["headers"] = maps.Clone(headers)
	}
}

func translateFlatProxyCredentials(out map[string]any, settings map[string]any) {
	if username := compatStringOption(settings, "user", "username"); username != "" {
		out["username"] = username
	}
	if password := compatStringOption(settings, "pass", "password"); password != "" {
		out["password"] = password
	}
}

func translateShadowsocksOutboundOptions(out map[string]any, settings map[string]any, tag string) error {
	server := firstObject(settings, "servers")
	if server == nil || !xrayBool(server, "uot") {
		return nil
	}
	version := xrayUoTVersion(server)
	if version != 1 && version != 2 {
		return fmt.Errorf("outbound %q has unsupported Shadowsocks UoT version %d", tag, version)
	}
	out["udp_over_tcp"] = map[string]any{"enabled": true, "version": version}
	return nil
}

func compatStringOption(settings map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(rawString(settings, key)); value != "" {
			return value
		}
	}
	return ""
}

func compatBoolOption(settings map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := settings[key].(bool); ok {
			return value, true
		}
	}
	return false, false
}

func translateTUICOutboundOptions(out map[string]any, settings map[string]any) {
	if value := compatStringOption(settings, "congestion_control", "congestionControl"); value != "" {
		out["congestion_control"] = strings.ToLower(value)
	}
	if value := compatStringOption(settings, "udp_relay_mode", "udpRelayMode"); value != "" {
		out["udp_relay_mode"] = strings.ToLower(value)
	}
	if value, ok := compatBoolOption(settings, "udp_over_stream", "udpOverStream"); ok {
		out["udp_over_stream"] = value
	}
	if value, ok := compatBoolOption(settings, "zero_rtt_handshake", "zeroRttHandshake"); ok {
		out["zero_rtt_handshake"] = value
	}
	if value := compatStringOption(settings, "heartbeat"); value != "" {
		out["heartbeat"] = value
	}
	if value := compatStringOption(settings, "network"); value != "" {
		out["network"] = strings.ToLower(value)
	}
}

func translateGRPCTransportCompatibility(out map[string]any, stream map[string]any, tag string) error {
	transport, ok := out["transport"].(map[string]any)
	if !ok || !strings.EqualFold(strings.TrimSpace(rawString(transport, "type")), "grpc") {
		return nil
	}
	grpc := rawObject(stream, "grpcSettings")
	if len(grpc) == 0 {
		return nil
	}
	if authority := strings.TrimSpace(rawString(grpc, "authority")); authority != "" {
		return fmt.Errorf("outbound %q uses Xray gRPC authority %q which sing-box V2Ray gRPC transport cannot represent", tag, authority)
	}
	if userAgent := strings.TrimSpace(rawString(grpc, "user_agent")); userAgent != "" {
		return fmt.Errorf("outbound %q uses Xray gRPC user_agent which sing-box V2Ray gRPC transport cannot represent", tag)
	}
	if xrayBool(grpc, "multiMode") {
		return fmt.Errorf("outbound %q enables Xray gRPC multiMode which is not wire-compatible with sing-box gRPC transport", tag)
	}
	if window := rawInt(grpc, "initial_windows_size"); window > 0 {
		return fmt.Errorf("outbound %q uses Xray gRPC initial_windows_size=%d which sing-box V2Ray gRPC transport cannot represent", tag, window)
	}
	if idle := rawInt(grpc, "idle_timeout"); idle > 0 {
		if idle < 10 {
			idle = 10
		}
		transport["idle_timeout"] = fmt.Sprintf("%ds", idle)
	}
	if timeout := rawInt(grpc, "health_check_timeout"); timeout > 0 {
		transport["ping_timeout"] = fmt.Sprintf("%ds", timeout)
	}
	if permit, ok := grpc["permit_without_stream"].(bool); ok {
		transport["permit_without_stream"] = permit
	}
	return nil
}

func normalizeXrayTLSCurve(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "curvep256", "secp256r1", "p256":
		return "P256"
	case "curvep384", "secp384r1", "p384":
		return "P384"
	case "curvep521", "secp521r1", "p521":
		return "P521"
	case "x25519":
		return "X25519"
	case "x25519mlkem768":
		return "X25519MLKEM768"
	default:
		return value
	}
}

func translateOutboundTLSCertificates(tlsOut, tlsIn map[string]any, tag string) error {
	certs, ok := tlsIn["certificates"].([]any)
	if !ok || len(certs) == 0 {
		return nil
	}

	// translateStream historically treated outbound certificates as server
	// certificates. Clear those fields and rebuild them according to Xray's
	// certificate usage so mTLS client credentials are not emitted as
	// server-only sing-box TLS fields.
	for _, key := range []string{"certificate", "certificate_path", "key", "key_path", "client_certificate", "client_certificate_path", "client_key", "client_key_path"} {
		delete(tlsOut, key)
	}

	clientCredentials := 0
	verifyCertificates := 0
	for index, item := range certs {
		cert, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("outbound %q TLS certificate %d is invalid", tag, index+1)
		}
		usage := strings.ToLower(strings.TrimSpace(rawString(cert, "usage")))
		if usage == "" {
			usage = "encipherment"
		}
		if rawInt(cert, "ocspStapling") > 0 || xrayBool(cert, "oneTimeLoading") || xrayBool(cert, "buildChain") {
			return fmt.Errorf("outbound %q TLS certificate %d uses Xray certificate lifecycle options which sing-box outbound TLS cannot represent", tag, index+1)
		}

		certificatePath := strings.TrimSpace(rawString(cert, "certificateFile"))
		certificate := compatStringSlice(cert["certificate"])
		keyPath := strings.TrimSpace(rawString(cert, "keyFile"))
		key := compatStringSlice(cert["key"])

		switch usage {
		case "verify":
			verifyCertificates++
			if verifyCertificates > 1 {
				return fmt.Errorf("outbound %q has multiple Xray TLS verify certificates; sing-box outbound TLS accepts one custom certificate chain", tag)
			}
			if certificatePath != "" {
				tlsOut["certificate_path"] = certificatePath
			} else if len(certificate) > 0 {
				tlsOut["certificate"] = certificate
			} else {
				return fmt.Errorf("outbound %q TLS verify certificate %d has no certificate data", tag, index+1)
			}
		case "encipherment":
			clientCredentials++
			if clientCredentials > 1 {
				return fmt.Errorf("outbound %q has multiple Xray TLS client certificates which cannot be represented by one sing-box TLS client identity", tag)
			}
			if certificatePath != "" {
				tlsOut["client_certificate_path"] = certificatePath
			} else if len(certificate) > 0 {
				tlsOut["client_certificate"] = certificate
			} else {
				return fmt.Errorf("outbound %q TLS client certificate %d has no certificate data", tag, index+1)
			}
			if keyPath != "" {
				tlsOut["client_key_path"] = keyPath
			} else if len(key) > 0 {
				tlsOut["client_key"] = key
			} else {
				return fmt.Errorf("outbound %q TLS client certificate %d has no private key", tag, index+1)
			}
		case "issue":
			return fmt.Errorf("outbound %q TLS certificate %d uses Xray issue mode which sing-box outbound TLS cannot represent", tag, index+1)
		default:
			return fmt.Errorf("outbound %q TLS certificate %d has unsupported usage %q", tag, index+1, usage)
		}
	}
	return nil
}

func translateOutboundTLSCompatibility(out map[string]any, stream map[string]any, tag string) error {
	if !strings.EqualFold(strings.TrimSpace(rawString(stream, "security")), "tls") {
		return nil
	}
	tlsIn := rawObject(stream, "tlsSettings")
	tlsOut, ok := out["tls"].(map[string]any)
	if !ok || tlsOut == nil {
		tlsOut = map[string]any{"enabled": true}
		out["tls"] = tlsOut
	}

	fingerprint := strings.ToLower(strings.TrimSpace(rawString(tlsIn, "fingerprint")))
	switch fingerprint {
	case "":
		// Xray defaults to the Chrome fingerprint when no explicit value is set.
		tlsOut["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
	case "unsafe":
		// Xray's special unsafe value means native Go TLS, not a uTLS fingerprint.
		delete(tlsOut, "utls")
	case "chrome", "firefox", "edge", "safari", "360", "qq", "ios", "android", "random", "randomized":
		tlsOut["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
	default:
		return fmt.Errorf("outbound %q uses Xray TLS fingerprint %q which sing-box uTLS cannot represent", tag, rawString(tlsIn, "fingerprint"))
	}
	if masterKeyLog := strings.TrimSpace(rawString(tlsIn, "masterKeyLog")); masterKeyLog != "" {
		return fmt.Errorf("outbound %q uses Xray TLS masterKeyLog which sing-box outbound TLS cannot represent", tag)
	}
	if echConfig := strings.TrimSpace(rawString(tlsIn, "echConfigList")); echConfig != "" {
		return fmt.Errorf("outbound %q uses Xray ECH configuration whose format is not compatible with sing-box TLS ECH", tag)
	}
	if echSockopt := rawObject(tlsIn, "echSockopt"); len(echSockopt) > 0 {
		return fmt.Errorf("outbound %q uses Xray ECH socket options which sing-box TLS cannot translate safely", tag)
	}

	if minVersion := strings.TrimSpace(rawString(tlsIn, "minVersion")); minVersion != "" {
		tlsOut["min_version"] = minVersion
	}
	if maxVersion := strings.TrimSpace(rawString(tlsIn, "maxVersion")); maxVersion != "" {
		tlsOut["max_version"] = maxVersion
	}
	if suites := strings.TrimSpace(rawString(tlsIn, "cipherSuites")); suites != "" {
		parts := strings.FieldsFunc(suites, func(r rune) bool { return r == ':' || r == ',' })
		values := make([]string, 0, len(parts))
		for _, value := range parts {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			tlsOut["cipher_suites"] = values
		}
	}
	if curves := rawStrings(tlsIn, "curvePreferences"); len(curves) > 0 {
		values := make([]string, 0, len(curves))
		for _, curve := range curves {
			if curve = normalizeXrayTLSCurve(curve); curve != "" {
				values = append(values, curve)
			}
		}
		if len(values) > 0 {
			tlsOut["curve_preferences"] = values
		}
	}
	if xrayBool(tlsIn, "disableSystemRoot") {
		return fmt.Errorf("outbound %q enables Xray disableSystemRoot which sing-box outbound TLS cannot represent per-outbound", tag)
	}
	if verifyName := strings.TrimSpace(rawString(tlsIn, "verifyPeerCertByName")); verifyName != "" {
		names := strings.Split(verifyName, ",")
		if len(names) != 1 {
			return fmt.Errorf("outbound %q uses multiple Xray TLS certificate verification names %q which sing-box cannot represent with one server_name", tag, verifyName)
		}
		verifyName = strings.TrimSpace(names[0])
		if verifyName == "" {
			return fmt.Errorf("outbound %q has an empty Xray TLS certificate verification name", tag)
		}
		serverName := strings.TrimSpace(rawString(tlsIn, "serverName"))
		if serverName != "" && !strings.EqualFold(serverName, verifyName) {
			return fmt.Errorf("outbound %q uses different Xray TLS SNI %q and certificate verification name %q which sing-box cannot represent separately", tag, serverName, verifyName)
		}
		tlsOut["server_name"] = verifyName
	}
	if pin := strings.TrimSpace(rawString(tlsIn, "pinnedPeerCertSha256")); pin != "" {
		return fmt.Errorf("outbound %q uses Xray full-certificate SHA-256 pinning which stable sing-box 1.14 cannot represent", tag)
	}
	if pins := rawStrings(tlsIn, "pinnedPeerCertSha256"); len(pins) > 0 {
		return fmt.Errorf("outbound %q uses Xray full-certificate SHA-256 pinning which stable sing-box 1.14 cannot represent", tag)
	}
	return translateOutboundTLSCertificates(tlsOut, tlsIn, tag)
}

func xrayV2RayUser(settings map[string]any) map[string]any {
	server := firstObject(settings, "vnext")
	if server == nil {
		return nil
	}
	users, _ := server["users"].([]any)
	if len(users) == 0 {
		return nil
	}
	user, _ := users[0].(map[string]any)
	return user
}

func xrayPacketEncoding(settings map[string]any) string {
	if value := compatStringOption(settings, "packetEncoding", "packet_encoding"); value != "" {
		return value
	}
	if user := xrayV2RayUser(settings); user != nil {
		return compatStringOption(user, "packetEncoding", "packet_encoding")
	}
	return ""
}

func translateV2RayPacketEncoding(out map[string]any, settings map[string]any, protocol, tag string) error {
	if protocol != "vless" && protocol != "vmess" {
		return nil
	}

	specialVisionUDP443 := false
	if protocol == "vless" {
		flow := strings.ToLower(strings.TrimSpace(rawString(out, "flow")))
		switch flow {
		case "":
		case "xtls-rprx-vision":
			out["flow"] = "xtls-rprx-vision"
		case "xtls-rprx-vision-udp443":
			out["flow"] = "xtls-rprx-vision"
			out["packet_encoding"] = "xudp"
			specialVisionUDP443 = true
		default:
			return fmt.Errorf("outbound %q VLESS has unsupported flow %q", tag, rawString(out, "flow"))
		}
	}

	value := strings.ToLower(strings.TrimSpace(xrayPacketEncoding(settings)))
	switch value {
	case "":
		return nil
	case "none":
		if !specialVisionUDP443 {
			delete(out, "packet_encoding")
		}
		return nil
	case "xudp":
		out["packet_encoding"] = "xudp"
		return nil
	case "packetaddr":
		if specialVisionUDP443 {
			return fmt.Errorf("outbound %q VLESS flow xtls-rprx-vision-udp443 conflicts with packetEncoding %q", tag, value)
		}
		out["packet_encoding"] = "packetaddr"
		return nil
	default:
		return fmt.Errorf("outbound %q %s has unsupported packetEncoding %q", tag, strings.ToUpper(protocol), value)
	}
}

func translateSendThrough(out map[string]any, raw map[string]any, tag string) error {
	value := strings.TrimSpace(rawString(raw, "sendThrough"))
	if value == "" || value == "0.0.0.0" || value == "::" {
		return nil
	}
	if strings.Contains(value, "/") {
		return fmt.Errorf("outbound %q uses sendThrough CIDR %q which sing-box cannot represent", tag, value)
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return fmt.Errorf("outbound %q has invalid sendThrough address %q", tag, value)
	}
	if ip.To4() != nil {
		out["inet4_bind_address"] = ip.String()
	} else {
		out["inet6_bind_address"] = ip.String()
	}
	return nil
}

func xrayDomainResolver(value, tag string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "AsIs") {
		return nil, nil
	}
	resolver := map[string]any{"server": "local"}
	switch strings.ToLower(value) {
	case "useip", "forceip":
	case "useipv4", "forceipv4":
		resolver["strategy"] = "ipv4_only"
	case "useipv6", "forceipv6":
		resolver["strategy"] = "ipv6_only"
	case "useipv4v6", "forceipv4v6":
		resolver["strategy"] = "prefer_ipv4"
	case "useipv6v4", "forceipv6v4":
		resolver["strategy"] = "prefer_ipv6"
	default:
		return nil, fmt.Errorf("outbound %q uses unknown Xray domainStrategy %q", tag, value)
	}
	return resolver, nil
}

func translateRoutingMark(out map[string]any, sockopt map[string]any, tag string) error {
	value, exists := sockopt["mark"]
	if !exists || value == nil {
		return nil
	}
	switch mark := value.(type) {
	case string:
		mark = strings.TrimSpace(mark)
		if mark == "" || mark == "0" || strings.EqualFold(mark, "0x0") {
			return nil
		}
		if strings.HasPrefix(strings.ToLower(mark), "0x") {
			number := mark[2:]
			if _, err := strconv.ParseUint(number, 16, 32); err != nil {
				return fmt.Errorf("outbound %q has invalid routing mark %q", tag, mark)
			}
			out["routing_mark"] = mark
			return nil
		}
		parsed, err := strconv.ParseUint(mark, 10, 32)
		if err != nil {
			return fmt.Errorf("outbound %q has invalid routing mark %q", tag, mark)
		}
		out["routing_mark"] = int(parsed)
	default:
		numericMark := rawInt(sockopt, "mark")
		if numericMark < 0 {
			return fmt.Errorf("outbound %q has invalid negative routing mark", tag)
		}
		if numericMark > 0 {
			out["routing_mark"] = numericMark
		}
	}
	return nil
}

func translateOutboundSockopt(out map[string]any, stream map[string]any, tag string) error {
	sockopt := rawObject(stream, "sockopt")
	if len(sockopt) == 0 {
		return nil
	}
	if bindInterface := strings.TrimSpace(rawString(sockopt, "interface")); bindInterface != "" {
		out["bind_interface"] = bindInterface
	}
	if err := translateRoutingMark(out, sockopt, tag); err != nil {
		return err
	}
	if xrayEnabled(sockopt["tcpFastOpen"]) {
		out["tcp_fast_open"] = true
	}
	if xrayEnabled(sockopt["tcpMptcp"]) {
		out["tcp_multi_path"] = true
	}
	idle := rawInt(sockopt, "tcpKeepAliveIdle")
	interval := rawInt(sockopt, "tcpKeepAliveInterval")
	if idle < 0 || interval < 0 {
		out["disable_tcp_keep_alive"] = true
	} else {
		if idle > 0 {
			out["tcp_keep_alive"] = fmt.Sprintf("%ds", idle)
		}
		if interval > 0 {
			out["tcp_keep_alive_interval"] = fmt.Sprintf("%ds", interval)
		}
	}
	if detour := strings.TrimSpace(rawString(sockopt, "dialerProxy")); detour != "" {
		out["detour"] = detour
	}
	resolver, err := xrayDomainResolver(rawString(sockopt, "domainStrategy"), tag)
	if err != nil {
		return err
	}
	if resolver != nil {
		out["domain_resolver"] = resolver
	}
	return nil
}

func ensureOutboundDomainResolver(out map[string]any) {
	if _, exists := out["domain_resolver"]; exists {
		return
	}
	if strings.TrimSpace(rawString(out, "detour")) != "" {
		return
	}
	server := strings.TrimSpace(rawString(out, "server"))
	if server == "" {
		return
	}
	if ip := net.ParseIP(strings.Trim(server, "[]")); ip != nil {
		return
	}
	out["domain_resolver"] = "local"
}

func wireGuardPeerUsesDomain(peer map[string]any) bool {
	address := strings.TrimSpace(rawString(peer, "address"))
	if address == "" {
		return false
	}
	return net.ParseIP(strings.Trim(address, "[]")) == nil
}

func ensureWireGuardEndpointDomainResolver(endpoint map[string]any) {
	if _, exists := endpoint["domain_resolver"]; exists {
		return
	}
	if strings.TrimSpace(rawString(endpoint, "detour")) != "" {
		return
	}
	switch peers := endpoint["peers"].(type) {
	case []map[string]any:
		for _, peer := range peers {
			if wireGuardPeerUsesDomain(peer) {
				endpoint["domain_resolver"] = "local"
				return
			}
		}
	case []any:
		for _, item := range peers {
			peer, ok := item.(map[string]any)
			if ok && wireGuardPeerUsesDomain(peer) {
				endpoint["domain_resolver"] = "local"
				return
			}
		}
	}
}

func applyXrayWireGuardEndpointCompatibility(endpoint map[string]any, raw map[string]any) error {
	tag := rawString(raw, "tag")
	if err := validateXrayOutboundOnlyOptions(raw, "wireguard", tag); err != nil {
		return err
	}
	if err := translateSendThrough(endpoint, raw, tag); err != nil {
		return err
	}
	if err := translateOutboundSockopt(endpoint, rawObject(raw, "streamSettings"), tag); err != nil {
		return err
	}
	ensureWireGuardEndpointDomainResolver(endpoint)
	return nil
}

func ensureRequiredOutboundTLS(out map[string]any, protocol string) {
	if protocol != "tuic" {
		return
	}
	tls, ok := out["tls"].(map[string]any)
	if !ok || tls == nil {
		tls = map[string]any{}
		out["tls"] = tls
	}
	if _, exists := tls["enabled"]; !exists {
		tls["enabled"] = true
	}
}

func validateXrayOutboundOnlyOptions(raw map[string]any, protocol, tag string) error {
	mux := rawObject(raw, "mux")
	if xrayBool(mux, "enabled") {
		return fmt.Errorf("outbound %q enables Xray Mux.Cool; sing-box multiplex is a different protocol", tag)
	}
	stream := rawObject(raw, "streamSettings")
	if strings.EqualFold(strings.TrimSpace(rawString(stream, "network")), "quic") {
		return fmt.Errorf("outbound %q uses Xray QUIC transport, which is not wire-compatible with sing-box V2Ray QUIC", tag)
	}
	strategy := strings.TrimSpace(rawString(raw, "targetStrategy"))
	if protocol != "freedom" && strategy != "" && !strings.EqualFold(strategy, "AsIs") {
		return fmt.Errorf("outbound %q uses Xray targetStrategy %q which has no equivalent sing-box outbound option", tag, strategy)
	}
	return nil
}

func applyXrayOutboundCompatibility(out map[string]any, raw map[string]any, stream map[string]any) error {
	protocol := strings.ToLower(strings.TrimSpace(rawString(raw, "protocol")))
	tag := rawString(raw, "tag")
	settings := rawObject(raw, "settings")

	if err := validateXrayOutboundOnlyOptions(raw, protocol, tag); err != nil {
		return err
	}
	switch protocol {
	case "socks":
		translateFlatProxyCredentials(out, settings)
	case "http":
		translateFlatProxyCredentials(out, settings)
		translateHTTPOutboundOptions(out, settings)
	case "shadowsocks":
		if err := translateShadowsocksOutboundOptions(out, settings, tag); err != nil {
			return err
		}
	case "vmess", "vless":
		if err := translateV2RayPacketEncoding(out, settings, protocol, tag); err != nil {
			return err
		}
	case "tuic":
		translateTUICOutboundOptions(out, settings)
	}
	ensureRequiredOutboundTLS(out, protocol)
	if rawString(out, "type") == "block" {
		return nil
	}
	if err := translateOutboundTLSCompatibility(out, stream, tag); err != nil {
		return err
	}
	if err := translateGRPCTransportCompatibility(out, stream, tag); err != nil {
		return err
	}
	if err := translateSendThrough(out, raw, tag); err != nil {
		return err
	}
	if err := translateOutboundSockopt(out, stream, tag); err != nil {
		return err
	}
	ensureOutboundDomainResolver(out)
	return nil
}
