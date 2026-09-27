package sub

import (
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
)

type LinkProvider struct {
	settingService service.SettingService
}

func NewLinkProvider() *LinkProvider {
	return &LinkProvider{}
}

func (p *LinkProvider) build(host string) *SubService {
	remarkTemplate, _ := p.settingService.GetRemarkTemplate()
	svc := NewSubService(remarkTemplate)
	svc.PrepareForRequest(host)
	return svc
}

func (p *LinkProvider) SubLinksForSubId(host, subId string) ([]string, error) {
	svc := p.build(host)
	links, _, _, _, err := svc.GetSubs(subId, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, splitLinkLines(l)...)
	}
	return out, nil
}

func (p *LinkProvider) LinksForClient(host string, inbound *model.Inbound, email string) []string {
	if !sudokuInboundUsable(inbound) {
		return nil
	}
	svc := p.build(host)
	svc.refreshSudokuCredentials(inbound)
	svc.projectThroughFallbackMaster(inbound)
	if endpoints := svc.hostEndpoints(inbound, "raw"); len(endpoints) > 0 {
		if client, ok := svc.clientForLink(inbound, email); ok {
			return splitLinkLines(svc.linkFromHosts(inbound, client, endpoints))
		}
	}
	return splitLinkLines(svc.GetLink(inbound, email))
}

func (p *LinkProvider) LinksForInbounds(host string, inbounds []*model.Inbound) []string {
	svc := p.build(host)
	var out []string
	for _, inbound := range inbounds {
		if !sudokuInboundUsable(inbound) {
			continue
		}
		svc.refreshSudokuCredentials(inbound)
		out = append(out, svc.inboundLinks(inbound)...)
	}
	return out
}

// splitLinkLines normalizes one generated multi-link entry into the lines that
// can be placed in a generic multi-protocol subscription.
//
// Mieru is a special case. The generator keeps both the native full-profile
// mieru:// form and the interoperable simple-sharing mierus:// form internally.
// They describe the same endpoint. Generic subscription parsers such as
// Hiddify/ray2sing treat both schemes as simple URLs, so feeding the native
// protobuf mieru:// value produces a second, invalid Mieru node. Prefer the
// simple-sharing form whenever the pair is present; a lone native link is kept
// so callers that explicitly provide only that form do not lose it.
func splitLinkLines(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	hasSimpleMieru := false
	for _, part := range parts {
		if strings.HasPrefix(strings.TrimSpace(part), "mierus://") {
			hasSimpleMieru = true
			break
		}
	}

	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if hasSimpleMieru && strings.HasPrefix(p, "mieru://") {
			continue
		}
		if _, duplicate := seen[p]; duplicate {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
