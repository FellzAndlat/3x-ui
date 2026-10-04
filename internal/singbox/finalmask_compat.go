package singbox

import (
	"fmt"
	"strconv"
	"strings"
)

// Translate only masks whose wire formats are supported natively. In
// particular, gecko is encoded as a salamander mask with packetSize in Xray.
func translateFinalMask(out, stream map[string]any, protocol string, inbound bool) error {
	mask := rawObject(stream, "finalmask")
	if len(mask) == 0 {
		return nil
	}
	label := fmt.Sprintf("connection %q finalmask", rawString(out, "tag"))
	if err := rejectXrayFields(mask, label, "tcp"); err != nil {
		return err
	}
	if protocol != "hysteria2" && protocol != "hysteria" {
		return rejectXrayFields(mask, label, "udp", "quicParams")
	}
	udp, ok := mask["udp"].([]any)
	if mask["udp"] != nil && !ok {
		return fmt.Errorf("%s has invalid UDP masks", label)
	}
	seen := map[string]bool{}
	for _, item := range udp {
		entry, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s has an invalid mask", label)
		}
		kind := rawString(entry, "type")
		if seen[kind] {
			return fmt.Errorf("%s contains repeated %s masks", label, kind)
		}
		seen[kind] = true
		settings := rawObject(entry, "settings")
		switch kind {
		case "salamander":
			if protocol != "hysteria2" || out["obfs"] != nil {
				return fmt.Errorf("%s: conflicting or unsupported obfuscation", label)
			}
			password := rawString(settings, "password")
			if password == "" {
				return fmt.Errorf("%s salamander requires a password", label)
			}
			obfs := map[string]any{"type": "salamander", "password": password}
			if size := rawString(settings, "packetSize"); size != "" {
				parts := strings.Split(size, "-")
				if len(parts) != 2 {
					return fmt.Errorf("%s has invalid gecko packetSize", label)
				}
				min, e1 := strconv.Atoi(parts[0])
				max, e2 := strconv.Atoi(parts[1])
				if e1 != nil || e2 != nil || min < 1 || max < min || max > 2048 {
					return fmt.Errorf("%s has invalid gecko packetSize", label)
				}
				obfs["type"], obfs["min_packet_size"], obfs["max_packet_size"] = "gecko", min, max
			}
			out["obfs"] = obfs
		case "udphop":
			if inbound || rawString(settings, "mode") != "intervalremote" {
				return fmt.Errorf("%s: unsupported UDP hopping mode", label)
			}
			if out["server_ports"] != nil {
				return fmt.Errorf("%s: conflicting port hopping settings", label)
			}
			ports := rawString(settings, "remotePorts")
			if ports == "" {
				return fmt.Errorf("%s UDP hopping requires remotePorts", label)
			}
			// sing-box port ranges use ':', Xray remotePorts uses '-'.
			out["server_ports"] = strings.Split(strings.ReplaceAll(ports, "-", ":"), ",")
			delete(out, "server_port")
			interval := strings.Split(rawString(settings, "interval"), "-")
			if len(interval) < 1 || len(interval) > 2 {
				return fmt.Errorf("%s has invalid hopping interval", label)
			}
			min, err := strconv.Atoi(interval[0])
			if err != nil || min <= 0 {
				return fmt.Errorf("%s has invalid hopping interval", label)
			}
			out["hop_interval"] = fmt.Sprintf("%ds", min)
			if len(interval) == 2 {
				max, err := strconv.Atoi(interval[1])
				if err != nil || max < min || protocol != "hysteria2" {
					return fmt.Errorf("%s has unsupported hopping interval range", label)
				}
				out["hop_interval_max"] = fmt.Sprintf("%ds", max)
			}
		default:
			return fmt.Errorf("%s: unsupported UDP mask %q", label, kind)
		}
	}
	quic := rawObject(mask, "quicParams")
	if err := rejectXrayFields(quic, label, "initStreamReceiveWindow", "initConnectionReceiveWindow"); err != nil {
		return err
	}
	for _, pair := range [][2]string{{"maxStreamReceiveWindow", "stream_receive_window"}, {"maxConnectionReceiveWindow", "connection_receive_window"}, {"maxIncomingStreams", "max_concurrent_streams"}} {
		if value := quic[pair[0]]; isMeaningfulCompatValue(value) {
			out[pair[1]] = value
		}
	}
	for _, pair := range [][2]string{{"maxIdleTimeout", "idle_timeout"}, {"keepAlivePeriod", "keep_alive_period"}} {
		if value := rawInt(quic, pair[0]); value > 0 {
			out[pair[1]] = fmt.Sprintf("%ds", value)
		}
	}
	return nil
}
