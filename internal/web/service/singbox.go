package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/amneziawg"
	"github.com/SawaMEN/3x-ui/v3/internal/amneziawgnet"
	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
	"github.com/SawaMEN/3x-ui/v3/internal/tuic"
	"github.com/SawaMEN/3x-ui/v3/internal/util/tail"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
	"github.com/SawaMEN/3x-ui/v3/internal/xray/geodata"
)

var (
	singBoxInboundService InboundService
	singBoxSettingService SettingService
	singBoxProcess        = singbox.NewProcess(singbox.GetConfigPath())
	singBoxTrafficAPI     = singbox.NewConnectionAPIClient()
	singBoxTrafficMu      sync.Mutex
	// A snapshot consumes native API deltas. Keep an unsuccessful DB batch so
	// the next poll can persist it before reading another snapshot.
	singBoxPendingInbound []*xray.Traffic
	singBoxPendingClients []*xray.ClientTraffic
	singBoxInstallMu      sync.Mutex
	singBoxApplyMu        sync.Mutex
	singBoxTemplateMu     sync.Mutex
)

// SetSingBoxDependencies wires the panel services used by the sing-box
// config generator. It mirrors the existing Xray service lifecycle wiring.
func SetSingBoxDependencies(inbound *InboundService, settings *SettingService) {
	if inbound != nil {
		singBoxInboundService = *inbound
	}
	if settings != nil {
		singBoxSettingService = *settings
	}
}

type SingBoxService struct{}

func singBoxInboundRequiresUsers(protocol model.Protocol) bool {
	switch protocol {
	case model.VLESS, model.VMESS, model.Shadowsocks, model.Trojan, model.Snell, model.NaiveProxy, model.Hysteria, model.ShadowTLS, model.AnyTLS, model.TUIC:
		return true
	default:
		return false
	}
}

// These listeners run in their own local processes, outside sing-box.
// Never emit them as sing-box inbounds or reserve sing-box client lookups for them.
func isLocalSidecarInbound(protocol model.Protocol) bool {
	switch protocol {
	case model.MTProto, model.AmneziaWG, model.Mieru,
		model.Pingtunnel, model.TrustTunnel, model.FPTN, model.OpenFlux, model.Sudoku, model.VKTurnProxy:
		return true
	default:
		return false
	}
}

func mustJSON(value map[string]any) []byte {
	data, _ := json.Marshal(value)
	return data
}

// singBoxTUICInbound uses the same credentials and TLS certificate as the
// external TUIC server. The native listener exposes authenticated user names
// to the sing-box traffic API, so client quotas receive real byte deltas.
func singBoxTUICInbound(ib *model.Inbound, clients []any) (map[string]any, error) {
	inst, ok := tuic.InstanceFromInbound(ib)
	if !ok {
		return nil, fmt.Errorf("invalid TUIC inbound %q settings", ib.Tag)
	}
	if strings.TrimSpace(inst.Certificate) == "" || strings.TrimSpace(inst.PrivateKey) == "" {
		return nil, fmt.Errorf("TUIC inbound %q requires a TLS certificate and private key", ib.Tag)
	}
	users := make([]map[string]any, 0, len(clients))
	for _, item := range clients {
		client, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := client["email"].(string)
		uuid, _ := client["uuid"].(string)
		password, _ := client["password"].(string)
		if name == "" || uuid == "" || password == "" {
			return nil, fmt.Errorf("TUIC inbound %q has an active client without email, UUID or password", ib.Tag)
		}
		users = append(users, map[string]any{"name": name, "uuid": uuid, "password": password})
	}
	listen := strings.TrimSpace(inst.Listen)
	if listen == "" {
		listen = "0.0.0.0"
	}
	if inst.Port < 1 || inst.Port > 65535 {
		return nil, fmt.Errorf("TUIC inbound %q has an invalid port", ib.Tag)
	}
	tls := map[string]any{"enabled": true, "alpn": inst.ALPN}
	if strings.Contains(inst.Certificate, "-----BEGIN CERTIFICATE-----") {
		tls["certificate"] = strings.Split(inst.Certificate, "\n")
		tls["key"] = strings.Split(inst.PrivateKey, "\n")
	} else {
		tls["certificate_path"], tls["key_path"] = inst.Certificate, inst.PrivateKey
	}
	return map[string]any{
		"type": "tuic", "tag": ib.Tag, "listen": listen, "listen_port": inst.Port,
		"users": users, "congestion_control": inst.CongestionControl,
		"auth_timeout":       fmt.Sprintf("%ds", inst.AuthenticationTimeout),
		"zero_rtt_handshake": inst.ZeroRTTHandshake,
		"tls":                tls,
	}, nil
}

func (s *SingBoxService) GetConfig() (*singbox.Config, error) {
	inbounds, err := singBoxInboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}

	cfg := singbox.NewConfig()
	// Generated outbounds come from the Xray template. Keep NewConfig's
	// defaults only for its standalone editor fallback.
	cfg.Outbounds = nil
	if singBoxProcess.SupportsNativeAPI() {
		cfg.Services = []map[string]any{{
			"type":        "api",
			"tag":         "panel-api",
			"listen":      "127.0.0.1",
			"listen_port": 10091,
		}}
	}

	managedOutboundTags := map[string]bool{}
	dnsOutboundTags := make(map[string]struct{})
	template, templateErr := singBoxSettingService.GetXrayConfigTemplate()
	if templateErr != nil {
		return nil, templateErr
	}
	{
		var xrayCfg map[string]any
		if err := json.Unmarshal([]byte(template), &xrayCfg); err != nil {
			return nil, fmt.Errorf("parse Xray template: %w", err)
		}
		{
			prepend, tail, err := (&OutboundSubscriptionService{}).activeOutboundsSplit()
			if err != nil {
				return nil, err
			}
			manual, _ := xrayCfg["outbounds"].([]any)
			xrayCfg["outbounds"] = append(append(prepend, manual...), tail...)
			if rawDNS, ok := xrayCfg["dns"].(map[string]any); ok && len(rawDNS) > 0 {
				rawRouting, _ := xrayCfg["routing"].(map[string]any)
				if err := singbox.ValidateXrayDNSRouting(rawDNS, rawRouting); err != nil {
					return nil, fmt.Errorf("sing-box DNS routing: %w", err)
				}
				dns, err := singbox.TranslateXrayDNS(rawDNS)
				if err != nil {
					return nil, fmt.Errorf("sing-box DNS template: %w", err)
				}
				if len(dns) > 0 {
					// sing-box 1.14 requires a resolver for domain-based outbound
					// server addresses. Keep the system resolver available even when
					// the Xray DNS template replaces the generated default.
					servers, _ := dns["servers"].([]map[string]any)
					hasLocal := false
					for _, server := range servers {
						if tag, _ := server["tag"].(string); tag == "local" {
							hasLocal = true
							break
						}
					}
					if !hasLocal {
						servers = append(servers, map[string]any{"type": "local", "tag": "local"})
						dns["servers"] = servers
					}
					cfg.DNS = dns
				}
			}
			if rawRouting, ok := xrayCfg["routing"].(map[string]any); ok && len(rawRouting) > 0 {
				if route, err := singbox.TranslateXrayRoutingWithGeoData(rawRouting, geodata.NewStore(assetDir())); err != nil {
					return nil, err
				} else if len(route) > 0 {
					cfg.Route = route
				}
			}
			// sing-box 1.14 requires a resolver for domain-based outbound
			// server addresses. Apply this after translated routing so the
			// compatibility route cannot accidentally discard it.
			cfg.Route["default_domain_resolver"] = cfg.DNS["final"]
			if rawRouting, ok := xrayCfg["routing"].(map[string]any); ok {
				if strategy := singbox.TranslateXrayDomainStrategy(fmt.Sprint(rawRouting["domainStrategy"])); strategy != "" {
					cfg.Route["default_domain_resolver"] = map[string]any{
						"server":   cfg.DNS["final"],
						"strategy": strategy,
					}
				}
			}
			if rawBalancers, ok := xrayCfg["routing"].(map[string]any); ok {
				var available []map[string]any
				for _, value := range xrayCfg["outbounds"].([]any) {
					if outbound, ok := value.(map[string]any); ok {
						available = append(available, outbound)
					}
				}
				balancers, err := singbox.TranslateXrayBalancers(rawBalancers, available...)
				if err != nil {
					return nil, err
				}
				cfg.Outbounds = append(cfg.Outbounds, balancers...)
			}
			if rawOutbounds, ok := xrayCfg["outbounds"].([]any); ok {
				if len(rawOutbounds) > 0 {
					if first, ok := rawOutbounds[0].(map[string]any); ok {
						if tag, ok := first["tag"].(string); ok && tag != "" {
							cfg.Route["final"] = tag
						}
					}
				}

				for i, raw := range rawOutbounds {
					ob, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if id, ok := automaticAliasID(ob); ok {
						sub, err := (&OutboundSubscriptionService{}).Get(id)
						if err != nil {
							return nil, err
						}
						var members []any
						if sub.LastFetchedOutbounds != "" {
							if err := json.Unmarshal([]byte(sub.LastFetchedOutbounds), &members); err != nil {
								return nil, err
							}
						}
						members = filterSubscriptionOutbounds("sing-box automatic selector", members)
						cfg.Outbounds = append(cfg.Outbounds, automaticSingBoxSelector(sub, members))
						continue
					}
					if protocol, _ := ob["protocol"].(string); strings.EqualFold(strings.TrimSpace(protocol), "dns") {
						settings, _ := ob["settings"].(map[string]any)
						for _, key := range []string{"address", "port", "rewriteAddress", "rewritePort", "nonIPQuery"} {
							if value, present := settings[key]; present && fmt.Sprint(value) != "" && fmt.Sprint(value) != "0" && fmt.Sprint(value) != "skip" {
								return nil, fmt.Errorf("DNS outbound %v option %s cannot be preserved by hijack-dns", ob["tag"], key)
							}
						}
						if tag, _ := ob["tag"].(string); strings.TrimSpace(tag) != "" {
							dnsOutboundTags[tag] = struct{}{}
						}
						continue
					}
					if protocol, _ := ob["protocol"].(string); strings.EqualFold(strings.TrimSpace(protocol), "wireguard") {
						endpoint, err := singbox.TranslateXrayWireGuardEndpoint(ob)
						if err != nil {
							return nil, err
						}
						cfg.Endpoints = append(cfg.Endpoints, endpoint)
						continue
					}
					if amneziawg.IsAmneziaWGOutbound(mustJSON(ob)) {
						bridged, bridgeOK := amneziawgnet.BuildSocksBridge(mustJSON(ob))
						if !bridgeOK {
							return nil, fmt.Errorf("amneziawg outbound %d: cannot create sing-box SOCKS bridge", i)
						}
						if err := json.Unmarshal(bridged, &ob); err != nil {
							return nil, fmt.Errorf("amneziawg outbound %d: invalid bridge: %w", i, err)
						}
					}
					if externalvpn.IsAdditionalOutbound(ob) {
						tag, _ := ob["tag"].(string)
						managedOutboundTags[tag] = true
						bridge, err := externalvpn.EnsureOutbound(ob)
						if err != nil {
							return nil, fmt.Errorf("outbound %q: %w", tag, err)
						}
						ob = bridge
					}
					translated, err := singbox.TranslateXrayOutbound(ob)
					if err != nil {
						return nil, err
					}
					cfg.Outbounds = append(cfg.Outbounds, translated)
				}
			}
			if cfg.Route != nil {
				singbox.RewriteDNSOutboundRoutes(cfg.Route, dnsOutboundTags)
			}
		}
	}

	hasDirect, hasBlocked := false, false
	for _, outbound := range cfg.Outbounds {
		tag, _ := outbound["tag"].(string)
		switch tag {
		case "direct":
			hasDirect = true
		case "blocked":
			hasBlocked = true
		}
	}
	if !hasDirect {
		cfg.Outbounds = append(cfg.Outbounds, map[string]any{"type": "direct", "tag": "direct"})
	}
	if !hasBlocked {
		cfg.Outbounds = append(cfg.Outbounds, map[string]any{"type": "block", "tag": "blocked"})
	}

	inboundIDs := make([]int, 0, len(inbounds))
	for _, inbound := range inbounds {
		if inbound == nil || !inbound.Enable || inbound.NodeID != nil || isLocalSidecarInbound(inbound.Protocol) {
			continue
		}
		inboundIDs = append(inboundIDs, inbound.Id)
	}
	clientsByInbound, err := singBoxInboundService.clientService.ListForInbounds(nil, inboundIDs)
	if err != nil {
		return nil, err
	}

	var unsupported []string
	var sniffRules []any
	for _, inbound := range inbounds {
		if inbound == nil || !inbound.Enable || inbound.NodeID != nil || isLocalSidecarInbound(inbound.Protocol) {
			continue
		}
		runtimeInbound, err := singBoxInboundService.buildInboundForNodePush(database.GetDB(), inbound)
		if err != nil {
			return nil, err
		}
		rawBytes, err := json.Marshal(runtimeInbound)
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal(rawBytes, &raw); err != nil {
			return nil, err
		}
		stream, _ := raw["streamSettings"].(map[string]any)
		network, _ := stream["network"].(string)
		if strings.EqualFold(strings.TrimSpace(network), "xhttp") {
			unsupported = append(unsupported, fmt.Sprintf("%s: XHTTP transport is only supported by Xray", inbound.Tag))
			continue
		}
		// Xray treats an empty inbound listen address as all interfaces. The
		// sing-box default is loopback, which makes a successfully started
		// inbound unreachable from the network. Preserve Xray semantics.
		if listen, ok := raw["listen"].(string); !ok || strings.TrimSpace(listen) == "" {
			raw["listen"] = "0.0.0.0"
		}
		dbClients := clientsByInbound[inbound.Id]
		// Snell user keys belong to this inbound. Keep the runtime credentials
		// identical to the native subscription even when the global client has
		// a different password for another protocol.
		snellKeys := map[string]string{}
		if inbound.Protocol == model.Snell {
			var local struct {
				Clients []model.Client `json:"clients"`
			}
			if err := json.Unmarshal([]byte(inbound.Settings), &local); err != nil {
				return nil, err
			}
			for _, client := range local.Clients {
				if client.Enable {
					snellKeys[client.Email] = client.Password
				}
			}
		}

		enableMap := make(map[string]bool, len(inbound.ClientStats))
		for _, stat := range inbound.ClientStats {
			enableMap[stat.Email] = stat.Enable
		}

		clients := make([]any, 0, len(dbClients))
		for _, client := range dbClients {
			if enabled, exists := enableMap[client.Email]; exists && !enabled {
				continue
			}
			if !client.Enable {
				continue
			}
			entry := map[string]any{"email": client.Email}
			switch inbound.Protocol {
			case model.VLESS:
				if client.ID != "" {
					entry["id"] = client.ID
				}
				if client.Flow != "" && !inbound.DisableFlow {
					entry["flow"] = client.Flow
				}
			case model.VMESS:
				if client.ID != "" {
					entry["id"] = client.ID
				}
				if client.Security != "" {
					entry["security"] = client.Security
				}
			case model.Snell:
				password, present := snellKeys[client.Email]
				if !present {
					continue
				}
				entry["password"] = password
			case model.Trojan:
				if client.Password != "" {
					entry["password"] = client.Password
				}
				if client.Flow != "" && !inbound.DisableFlow {
					entry["flow"] = client.Flow
				}
			case model.Shadowsocks:
				if client.Password != "" {
					entry["password"] = client.Password
				}
			case model.Hysteria:
				if client.Auth != "" {
					entry["auth"] = client.Auth
				}
			case model.TUIC:
				if client.ID != "" {
					entry["uuid"] = client.ID
				}
				if client.Password != "" {
					entry["password"] = client.Password
				}
			case model.HTTP, model.Mixed, model.NaiveProxy, model.Mieru, model.AnyTLS, model.ShadowTLS:
				if client.Password != "" {
					entry["password"] = client.Password
				}
			}
			clients = append(clients, entry)
		}
		managedProxy := (inbound.Protocol == model.HTTP || inbound.Protocol == model.Mixed) && len(dbClients) > 0
		if (singBoxInboundRequiresUsers(inbound.Protocol) || managedProxy) && len(clients) == 0 {
			logger.Warningf("Skipping sing-box inbound %q (%s): no active users", inbound.Tag, inbound.Protocol)
			continue
		}
		if inbound.Protocol == model.TUIC {
			translated, err := singBoxTUICInbound(inbound, clients)
			if err != nil {
				unsupported = append(unsupported, fmt.Sprintf("%s: %v", inbound.Tag, err))
				continue
			}
			cfg.Inbounds = append(cfg.Inbounds, translated)
			continue
		}

		settings, _ := raw["settings"].(map[string]any)
		if settings == nil {
			settings = map[string]any{}
		}
		if managedProxy {
			// A stale accounts copy must not re-enable disabled managed users.
			delete(settings, "accounts")
		}
		settings["clients"] = clients
		sniffRule, sniffErr := singbox.TranslateXraySniffingRule(raw)
		if sniffErr != nil {
			unsupported = append(unsupported, sniffErr.Error())
			continue
		}
		if sniffRule != nil {
			sniffRules = append(sniffRules, sniffRule)
		}

		// NaiveProxy is a native TLS protocol in sing-box. Keep the ordinary
		// inbound form simple by reusing the panel's HTTPS certificate/key when
		// the inbound does not explicitly provide its own pair.
		if inbound.Protocol == model.NaiveProxy || inbound.Protocol == model.AnyTLS {
			tls, _ := settings["tls"].(map[string]any)
			if tls == nil {
				tls = map[string]any{}
			}
			tls["enabled"] = true
			certPath, _ := tls["certificatePath"].(string)
			certPath = strings.TrimSpace(certPath)
			keyPath, _ := tls["keyPath"].(string)
			keyPath = strings.TrimSpace(keyPath)
			if (certPath == "") != (keyPath == "") {
				protocolName := "NaiveProxy"
				if inbound.Protocol == model.AnyTLS {
					protocolName = "AnyTLS"
				}
				return nil, fmt.Errorf("%s inbound %q must provide both TLS certificate and private key, or neither", protocolName, inbound.Tag)
			}
			if certPath == "" {
				// Prefer the panel HTTPS certificate, then fall back to the
				// dedicated subscription HTTPS certificate. Both are valid
				// certificate sources for a native Naive TLS listener.
				webCertPath, _ := singBoxSettingService.GetCertFile()
				webKeyPath, _ := singBoxSettingService.GetKeyFile()
				webCertPath = strings.TrimSpace(webCertPath)
				webKeyPath = strings.TrimSpace(webKeyPath)
				if webCertPath != "" && webKeyPath != "" {
					certPath = webCertPath
					keyPath = webKeyPath
				} else {
					subCertPath, _ := singBoxSettingService.GetSubCertFile()
					subKeyPath, _ := singBoxSettingService.GetSubKeyFile()
					subCertPath = strings.TrimSpace(subCertPath)
					subKeyPath = strings.TrimSpace(subKeyPath)
					if subCertPath != "" && subKeyPath != "" {
						certPath = subCertPath
						keyPath = subKeyPath
					}
				}
			}
			if certPath == "" || keyPath == "" {
				protocolName := "NaiveProxy"
				if inbound.Protocol == model.AnyTLS {
					protocolName = "AnyTLS"
				}
				return nil, fmt.Errorf("%s inbound %q requires a complete TLS certificate/private-key pair (inbound, panel HTTPS, or subscription HTTPS)", protocolName, inbound.Tag)
			}
			tls["certificatePath"] = certPath
			tls["keyPath"] = keyPath
			settings["tls"] = tls
		}

		raw["settings"] = settings
		if model.ShadowTLSTransport(inbound.Settings) != nil {
			outer, inner, err := singbox.TranslateShadowTLSWrappedInbound(raw)
			if err != nil {
				unsupported = append(unsupported, fmt.Sprintf("%s: %v", inbound.Tag, err))
				continue
			}
			cfg.Inbounds = append(cfg.Inbounds, inner, outer)
			continue
		}

		translated, err := singbox.TranslateXrayInbound(raw)
		if err != nil {
			unsupported = append(unsupported, fmt.Sprintf("%s: %v", inbound.Tag, err))
			continue
		}
		if inbound.Protocol == model.ShadowTLS {
			inner, err := singbox.TranslateShadowTLSInnerInbound(raw)
			if err != nil {
				unsupported = append(unsupported, fmt.Sprintf("%s: %v", inbound.Tag, err))
				continue
			}
			// Route and account bytes under the panel identity, on the injected listener.
			inner["tag"] = inbound.Tag
			translated["tag"] = "__shadowtls_transport_" + inbound.Tag
			translated["detour"] = inbound.Tag
			cfg.Inbounds = append(cfg.Inbounds, inner)
		}
		cfg.Inbounds = append(cfg.Inbounds, translated)
	}
	if len(unsupported) > 0 {
		return nil, fmt.Errorf("sing-box cannot represent enabled inbounds: %s", strings.Join(unsupported, "; "))
	}
	if err := s.applyNativeTemplate(cfg); err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if err := injectSingBoxMtprotoEgress(cfg, inbound); err != nil {
			return nil, err
		}
	}
	if len(sniffRules) > 0 {
		if cfg.Route == nil {
			cfg.Route = map[string]any{}
		}
		cfg.Route["rules"] = append(sniffRules, singBoxRouteRules(cfg.Route)...)
	}
	ensureAutomaticClashAPI(cfg)
	applySingBoxInfrastructureEgress(cfg)
	if err := singBoxSettingService.applySingBoxAdBlock(cfg); err != nil {
		return nil, err
	}
	if err := singBoxSettingService.applySingBoxYouTubeServer(cfg); err != nil {
		return nil, err
	}
	externalvpn.KeepOutbounds(managedOutboundTags)
	return cfg, nil
}

type SingBoxEditorSnapshot struct {
	InboundTags     []string       `json:"inboundTags"`
	Config          map[string]any `json:"config"`
	Running         bool           `json:"running"`
	Version         string         `json:"version"`
	ConfigPath      string         `json:"configPath"`
	ConfigSource    string         `json:"configSource"`
	ConfigModified  string         `json:"configModified"`
	ManagedSections []string       `json:"managedSections"`
}

func (s *SingBoxService) GetEditorConfig(ctx context.Context) (*SingBoxEditorSnapshot, error) {
	path := singbox.GetConfigPath()
	data, err := os.ReadFile(path)
	source := "disk"
	if os.IsNotExist(err) {
		cfg, cfgErr := s.GetConfig()
		if cfgErr == nil {
			data, err = cfg.Marshal()
		} else {
			rawTemplate, templateErr := singBoxSettingService.GetSingBoxConfigTemplate()
			if templateErr != nil {
				return nil, cfgErr
			}
			rawTemplate = strings.TrimSpace(rawTemplate)
			if rawTemplate == "" {
				data, err = singbox.NewConfig().Marshal()
			} else {
				data = []byte(rawTemplate)
				err = nil
			}
		}
		source = "generated"
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	// Remove the named panel-managed filter from an editor round trip.
	var editorConfig singbox.Config
	if err := json.Unmarshal(data, &editorConfig); err != nil {
		return nil, err
	}
	if err := stripSingBoxAdBlock(&editorConfig); err != nil {
		return nil, err
	}
	if route, ok := raw["route"]; ok && route != nil {
		raw["route"] = editorConfig.Route
	}
	if _, ok := raw["outbounds"]; ok {
		raw["outbounds"] = editorConfig.Outbounds
	}
	inboundTags := make([]string, 0, len(editorConfig.Inbounds))
	for _, inbound := range editorConfig.Inbounds {
		if tag, ok := inbound["tag"].(string); ok && tag != "" {
			inboundTags = append(inboundTags, tag)
		}
	}
	delete(raw, "inbounds")
	delete(raw, "services")
	modified := ""
	if info, statErr := os.Stat(path); statErr == nil {
		modified = info.ModTime().UTC().Format(time.RFC3339)
	}
	version := "Unknown"
	if v, versionErr := s.CachedVersion(ctx); versionErr == nil && strings.TrimSpace(v) != "" {
		version = v
	}
	return &SingBoxEditorSnapshot{
		Config:          raw,
		InboundTags:     inboundTags,
		Running:         s.IsRunning(),
		Version:         version,
		ConfigPath:      path,
		ConfigSource:    source,
		ConfigModified:  modified,
		ManagedSections: []string{"inbounds", "services"},
	}, nil
}

func normalizeSingBoxTemplate(raw string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", fmt.Errorf("invalid sing-box JSON: %w", err)
	}
	if obj == nil {
		return "", fmt.Errorf("sing-box settings must be a JSON object")
	}
	allowed := map[string]struct{}{
		"$schema": {}, "log": {}, "dns": {}, "ntp": {}, "certificate": {},
		"certificate_providers": {}, "http_clients": {}, "network_namespaces": {},
		"outbounds": {}, "route": {}, "endpoints": {}, "experimental": {},
	}
	for key := range obj {
		if key == "inbounds" || key == "services" {
			delete(obj, key)
			continue
		}
		if _, ok := allowed[key]; !ok {
			return "", fmt.Errorf("unsupported sing-box top-level field %q; panel-managed inbounds/services are not editable here", key)
		}
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *SingBoxService) applyNativeTemplate(cfg *singbox.Config) error {
	rawTemplate, err := singBoxSettingService.GetSingBoxConfigTemplate()
	if err != nil {
		return err
	}
	rawTemplate = strings.TrimSpace(rawTemplate)
	if rawTemplate == "" {
		return nil
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawTemplate), &patch); err != nil {
		return fmt.Errorf("stored sing-box settings are invalid: %w", err)
	}
	currentData, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(currentData, &merged); err != nil {
		return err
	}
	for key, value := range patch {
		if key == "inbounds" || key == "services" {
			continue
		}
		merged[key] = value
	}
	mergedData, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(mergedData, cfg); err != nil {
		return err
	}
	var referencesLocal func(any) bool
	referencesLocal = func(value any) bool {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				if key == "domain_resolver" || key == "default_domain_resolver" {
					if name, ok := item.(string); ok && name == "local" {
						return true
					}
					if resolver, ok := item.(map[string]any); ok && resolver["server"] == "local" {
						return true
					}
					continue
				}
				if referencesLocal(item) {
					return true
				}
			}
		case []any:
			for _, item := range value {
				if referencesLocal(item) {
					return true
				}
			}
		case []map[string]any:
			for _, item := range value {
				if referencesLocal(item) {
					return true
				}
			}
		}
		return false
	}
	if referencesLocal(cfg.Inbounds) || referencesLocal(cfg.Route) || referencesLocal(cfg.Outbounds) || referencesLocal(cfg.Endpoints) {
		if cfg.DNS == nil {
			cfg.DNS = map[string]any{}
		}
		data, _ := json.Marshal(cfg.DNS["servers"])
		var servers []map[string]any
		if err := json.Unmarshal(data, &servers); err != nil && string(data) != "null" {
			return fmt.Errorf("invalid native DNS servers: %w", err)
		}
		hasLocal := false
		for _, server := range servers {
			if server["tag"] == "local" {
				hasLocal = true
			}
		}
		if !hasLocal {
			cfg.DNS["servers"] = append(servers, map[string]any{"type": "local", "tag": "local"})
		}
	}
	return nil
}

func (s *SingBoxService) SaveTemplate(ctx context.Context, raw string) error {
	singBoxTemplateMu.Lock()
	defer singBoxTemplateMu.Unlock()
	normalized, err := normalizeSingBoxTemplate(raw)
	if err != nil {
		return err
	}
	old, err := singBoxSettingService.GetSingBoxConfigTemplate()
	if err != nil {
		return err
	}
	if err := singBoxSettingService.SetSingBoxConfigTemplate(normalized); err != nil {
		return err
	}
	rollback := func() {
		_ = singBoxSettingService.SetSingBoxConfigTemplate(old)
		_ = s.WriteConfig()
	}
	if err := s.WriteConfig(); err != nil {
		rollback()
		return err
	}
	if !s.IsRunning() {
		return nil
	}
	if err := s.Validate(ctx); err != nil {
		rollback()
		return err
	}
	if err := s.Restart(ctx); err != nil {
		rollback()
		_ = s.Restart(ctx)
		return err
	}
	return nil
}

func (s *SingBoxService) ResetTemplate(ctx context.Context) error {
	singBoxTemplateMu.Lock()
	defer singBoxTemplateMu.Unlock()
	old, err := singBoxSettingService.GetSingBoxConfigTemplate()
	if err != nil {
		return err
	}
	if err := singBoxSettingService.SetSingBoxConfigTemplate(""); err != nil {
		return err
	}
	rollback := func() {
		_ = singBoxSettingService.SetSingBoxConfigTemplate(old)
		_ = s.WriteConfig()
	}
	if err := s.WriteConfig(); err != nil {
		rollback()
		return err
	}
	if !s.IsRunning() {
		return nil
	}
	if err := s.Validate(ctx); err != nil {
		rollback()
		return err
	}
	if err := s.Restart(ctx); err != nil {
		rollback()
		_ = s.Restart(ctx)
		return err
	}
	return nil
}

func (s *SingBoxService) WriteConfig() error {
	singBoxApplyMu.Lock()
	defer singBoxApplyMu.Unlock()
	return s.writeConfigCandidate(context.Background())
}

func singBoxConfigDir() string {
	path := singbox.GetConfigPath()
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

func (s *SingBoxService) Restart(ctx context.Context) error {
	singBoxApplyMu.Lock()
	defer singBoxApplyMu.Unlock()
	return s.restartLocked(ctx)
}

func (s *SingBoxService) restartLocked(ctx context.Context) error {
	// Capture the old applied config before publishing a candidate.
	_ = s.IsRunning()
	if _, err := singBoxProcess.Version(ctx); err != nil {
		singBoxProcess.SetError(err)
		return err
	}
	if err := s.writeConfigCandidate(ctx); err != nil {
		singBoxProcess.SetError(err)
		return err
	}
	tuic.GetManager().StopAll()
	if err := singBoxProcess.Restart(ctx); err != nil {
		return err
	}
	singBoxTrafficMu.Lock()
	singBoxTrafficAPI.Close()
	singBoxTrafficAPI.ResetTrafficBaseline(singBoxProcess.GetStartTime().UnixMilli())
	singBoxTrafficMu.Unlock()
	markSingBoxStarted()
	commitManagedYouTube(CoreTypeSingBox)
	return nil
}

func (s *SingBoxService) Start(ctx context.Context) error {
	singBoxApplyMu.Lock()
	defer singBoxApplyMu.Unlock()
	return s.startLocked(ctx)
}

func (s *SingBoxService) startLocked(ctx context.Context) error {
	if s.IsRunning() {
		return nil
	}
	if _, err := singBoxProcess.Version(ctx); err != nil {
		singBoxProcess.SetError(err)
		return err
	}
	if err := s.writeConfigCandidate(ctx); err != nil {
		singBoxProcess.SetError(err)
		return err
	}
	tuic.GetManager().StopAll()
	if err := singBoxProcess.Start(ctx); err != nil {
		return err
	}
	singBoxTrafficMu.Lock()
	singBoxTrafficAPI.Close()
	singBoxTrafficAPI.ResetTrafficBaseline(singBoxProcess.GetStartTime().UnixMilli())
	singBoxTrafficMu.Unlock()
	markSingBoxStarted()
	commitManagedYouTube(CoreTypeSingBox)
	return nil
}

func (s *SingBoxService) Stop(ctx context.Context) error {
	singBoxApplyMu.Lock()
	defer singBoxApplyMu.Unlock()
	return s.stopLocked(ctx)
}

func (s *SingBoxService) stopLocked(ctx context.Context) error {
	if err := singBoxProcess.Stop(); err != nil {
		return err
	}
	markSingBoxStopped()
	return nil
}

func (s *SingBoxService) IsRunning() bool {
	return singBoxProcess.IsRunning()
}

func (s *SingBoxService) LastError() error {
	return singBoxProcess.GetErr()
}

func (s *SingBoxService) Version(ctx context.Context) (string, error) {
	return singBoxProcess.Version(ctx)
}

func (s *SingBoxService) CachedVersion(ctx context.Context) (string, error) {
	if version := singBoxProcess.GetVersion(); version != "" && version != "Unknown" {
		return version, nil
	}
	return singBoxProcess.Version(ctx)
}

func (s *SingBoxService) Validate(ctx context.Context) error {
	return singBoxProcess.Validate(ctx)
}

func (s *SingBoxService) ConnectionCount(ctx context.Context) (int, error) {
	api := singbox.NewConnectionAPIClient()
	defer api.Close()
	connections, err := api.Snapshot(ctx)
	if err == nil {
		active := 0
		for _, connection := range connections {
			if connection != nil && connection.ClosedAt == 0 {
				active++
			}
		}
		return active, nil
	}
	clashConnections, err := singbox.NewClashStatsClient().Connections(ctx)
	if err != nil {
		return 0, err
	}
	return len(clashConnections), nil
}

// OnlinePresence returns the current sing-box users/IPs and active inbound
// tags from one native API snapshot. Keeping them in the same snapshot makes
// the node's online user and inbound indicators describe the same instant.
func (s *SingBoxService) OnlinePresence(ctx context.Context) (map[string]map[string]struct{}, []string, error) {
	api := singbox.NewConnectionAPIClient()
	defer api.Close()
	connections, err := api.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	online := make(map[string]map[string]struct{})
	activeSet := make(map[string]struct{})
	for _, connection := range connections {
		if connection == nil || connection.ClosedAt > 0 {
			continue
		}
		if connection.Inbound != "" {
			activeSet[connection.Inbound] = struct{}{}
		}
		if connection.User == "" || connection.Source == "" {
			continue
		}
		ip := connection.Source
		if host, _, splitErr := net.SplitHostPort(ip); splitErr == nil {
			ip = host
		}
		if ip == "" {
			continue
		}
		if online[connection.User] == nil {
			online[connection.User] = make(map[string]struct{})
		}
		online[connection.User][ip] = struct{}{}
	}
	active := make([]string, 0, len(activeSet))
	for tag := range activeSet {
		active = append(active, tag)
	}
	sort.Strings(active)
	return online, active, nil
}

func (s *SingBoxService) OnlineClientIPs(ctx context.Context) (map[string]map[string]struct{}, error) {
	online, _, err := s.OnlinePresence(ctx)
	return online, err
}

func (s *SingBoxService) DisconnectClientIPs(ctx context.Context, email string, ips []string) error {
	if email == "" || len(ips) == 0 {
		return nil
	}
	api := singbox.NewConnectionAPIClient()
	defer api.Close()
	connections, err := api.Snapshot(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		if ip != "" {
			wanted[ip] = struct{}{}
		}
	}
	var disconnectErrors []string
	for _, connection := range connections {
		if connection == nil || connection.ClosedAt > 0 || connection.User != email || connection.Source == "" {
			continue
		}
		ip := connection.Source
		if host, _, splitErr := net.SplitHostPort(ip); splitErr == nil {
			ip = host
		}
		if _, ok := wanted[ip]; !ok {
			continue
		}
		if err := api.CloseConnection(ctx, connection.ID); err != nil {
			disconnectErrors = append(disconnectErrors, err.Error())
		}
	}
	if len(disconnectErrors) > 0 {
		return fmt.Errorf("disconnect sing-box sessions: %s", strings.Join(disconnectErrors, "; "))
	}
	return nil
}

func accumulateSingBoxTraffic(up, down *int64, uplinkDelta, downlinkDelta int64) {
	*up = sumTrafficDelta(*up, uplinkDelta)
	*down = sumTrafficDelta(*down, downlinkDelta)
}

func (s *SingBoxService) PollTraffic(ctx context.Context) error {
	// The cron callback creates a lightweight SingBoxService value every 5s;
	// keep the native API transport package-wide so traffic polling does not
	// repeatedly allocate a gRPC ClientConn and its background transport state.
	singBoxTrafficMu.Lock()
	defer singBoxTrafficMu.Unlock()
	if len(singBoxPendingInbound) > 0 || len(singBoxPendingClients) > 0 {
		if err := s.commitTraffic(singBoxPendingInbound, singBoxPendingClients); err != nil {
			return err
		}
		singBoxPendingInbound = nil
		singBoxPendingClients = nil
	}
	response, err := singBoxTrafficAPI.SnapshotTrafficEvents(ctx)
	if err != nil {
		return err
	}

	inboundDeltas := make(map[string]*xray.Traffic)
	clientDeltas := make(map[string]*xray.ClientTraffic)
	onlineEmails := make(map[string]struct{})

	for _, event := range response.Events {
		if event == nil || event.Connection == nil {
			continue
		}
		connection := event.Connection
		if event.Type != singbox.ConnectionEventClosed && connection.User != "" {
			onlineEmails[connection.User] = struct{}{}
		}

		uplinkDelta := event.UplinkDelta
		downlinkDelta := event.DownlinkDelta
		if uplinkDelta == 0 && downlinkDelta == 0 {
			continue
		}

		if connection.Inbound != "" {
			traffic := inboundDeltas[connection.Inbound]
			if traffic == nil {
				traffic = &xray.Traffic{Tag: connection.Inbound, IsInbound: true}
				inboundDeltas[connection.Inbound] = traffic
			}
			accumulateSingBoxTraffic(&traffic.Up, &traffic.Down, uplinkDelta, downlinkDelta)
		}
		if connection.User != "" {
			traffic := clientDeltas[connection.User]
			if traffic == nil {
				traffic = &xray.ClientTraffic{Email: connection.User}
				clientDeltas[connection.User] = traffic
			}
			accumulateSingBoxTraffic(&traffic.Up, &traffic.Down, uplinkDelta, downlinkDelta)
		}
	}

	inboundTraffic := make([]*xray.Traffic, 0, len(inboundDeltas))
	for _, traffic := range inboundDeltas {
		inboundTraffic = append(inboundTraffic, traffic)
	}
	clientTraffic := make([]*xray.ClientTraffic, 0, len(clientDeltas))
	for _, traffic := range clientDeltas {
		clientTraffic = append(clientTraffic, traffic)
	}
	{
		if err = s.commitTraffic(inboundTraffic, clientTraffic); err != nil {
			singBoxPendingInbound = inboundTraffic
			singBoxPendingClients = clientTraffic
			return err
		}
	}

	emails := make([]string, 0, len(onlineEmails))
	for email := range onlineEmails {
		emails = append(emails, email)
	}
	sort.Strings(emails)
	if len(emails) > 0 {
		return singBoxInboundService.BumpClientsLastOnline(emails)
	}
	return nil
}

// GetLogs returns sing-box process log lines in the same shape used by the
// existing core log viewer. sing-box writes its structured runtime log to the
// panel-managed log file configured in Config.Log.output.
func (s *SingBoxService) GetLogs(count string, filter string) []LogEntry {
	limit, err := strconv.Atoi(count)
	if err != nil || limit <= 0 {
		limit = 100
	}
	lines, err := tail.ReadTailLines(filepath.Join(filepath.Dir(singbox.GetConfigPath()), "sing-box.log"), 0, tail.DefaultTailBytes)
	if err != nil {
		return []LogEntry{}
	}
	filter = strings.TrimSpace(filter)
	filterLower := strings.ToLower(filter)
	entries := make([]LogEntry, 0, min(limit, len(lines)))
	for _, rawLine := range lines {
		if len(entries) >= limit {
			break
		}
		line := strings.TrimSpace(rawLine)
		if line == "" || (filterLower != "" && !strings.Contains(strings.ToLower(line), filterLower)) {
			continue
		}
		entries = append(entries, LogEntry{
			DateTime:  time.Now(),
			Inbound:   "sing-box",
			Outbound:  "sing-box",
			ToAddress: line,
		})
	}
	return entries
}

func (s *SingBoxService) installVersion(ctx context.Context, installer func(context.Context) (string, error)) (string, error) {
	singBoxInstallMu.Lock()
	defer singBoxInstallMu.Unlock()
	singBoxApplyMu.Lock()
	defer singBoxApplyMu.Unlock()

	restore, cleanup, err := singbox.SnapshotInstallation()
	if err != nil {
		return "", fmt.Errorf("backup sing-box installation: %w", err)
	}
	keepBackup := false
	defer func() {
		if !keepBackup {
			cleanup()
		}
	}()
	wasRunning := s.IsRunning()
	if wasRunning {
		if err := s.stopLocked(ctx); err != nil {
			return "", fmt.Errorf("stop sing-box before update: %w", err)
		}
	}
	rollback := func(cause error) (string, error) {
		// The update request may have been cancelled; recovery must still work.
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := singBoxProcess.Stop(); err != nil {
			keepBackup = true
			return "", fmt.Errorf("%w; stop failed update: %w", cause, err)
		}
		if err := restore(); err != nil {
			keepBackup = true
			return "", fmt.Errorf("%w; restore previous installation: %w", cause, err)
		}
		if wasRunning {
			if _, err := singBoxProcess.Version(recoveryCtx); err != nil {
				return "", fmt.Errorf("%w; read restored version: %w", cause, err)
			}
			// Run the restored applied config, not a freshly generated candidate.
			if err := singBoxProcess.Start(recoveryCtx); err != nil {
				return "", fmt.Errorf("%w; restart restored installation: %w", cause, err)
			}
			markSingBoxStarted()
		}
		singBoxProcess.SetError(cause)
		return "", cause
	}
	installed, err := installer(ctx)
	if err != nil {
		return rollback(fmt.Errorf("install sing-box: %w", err))
	}
	currentVersion, err := s.Version(ctx)
	if err != nil {
		return rollback(fmt.Errorf("verify installed sing-box %q: %w", installed, err))
	}
	if wasRunning {
		if err := s.startLocked(ctx); err != nil {
			return rollback(fmt.Errorf("start updated sing-box %s: %w", currentVersion, err))
		}
	}
	return currentVersion, nil
}

func (s *SingBoxService) InstallLatest(ctx context.Context) (string, error) {
	return s.installVersion(ctx, singbox.InstallLatest)
}

func (s *SingBoxService) ListVersions(ctx context.Context) ([]singbox.ReleaseVersion, error) {
	return singbox.ListVersions(ctx)
}

func (s *SingBoxService) InstallVersion(ctx context.Context, version string) (string, error) {
	return s.installVersion(ctx, func(ctx context.Context) (string, error) {
		return singbox.InstallVersion(ctx, version)
	})
}

func (s *SingBoxService) Uninstall(ctx context.Context) error {
	if s.IsRunning() {
		return fmt.Errorf("stop sing-box before uninstalling it")
	}
	return singbox.Uninstall()
}

func (s *SingBoxService) BinaryPath() string {
	return singbox.GetBinaryPath()
}

func (s *SingBoxService) ProcessConfigPath() string {
	return singbox.GetConfigPath()
}

func (s *SingBoxService) commitTraffic(inbounds []*xray.Traffic, clients []*xray.ClientTraffic) error {
	needRestart, disabled, err := singBoxInboundService.AddTraffic(inbounds, clients)
	if needRestart || disabled {
		s.SetToNeedRestart()
	}
	return err
}

func init() { singbox.SetRuntimeConfigReader(singBoxProcess.AppliedConfig) }
