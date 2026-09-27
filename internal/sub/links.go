package sub

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
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
	return normalizeGeneratedLinks(links), nil
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
	var raw []string
	for _, inbound := range inbounds {
		if !sudokuInboundUsable(inbound) {
			continue
		}
		svc.refreshSudokuCredentials(inbound)
		raw = append(raw, svc.inboundLinks(inbound)...)
	}
	return normalizeGeneratedLinks(raw)
}

// normalizeGeneratedLinks flattens multi-link entries and removes exact
// duplicates while preserving their first-seen order. This is shared by the
// raw subscription endpoint and link-export APIs so clients see the same set.
func normalizeGeneratedLinks(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		for _, link := range splitLinkLines(entry) {
			if _, duplicate := seen[link]; duplicate {
				continue
			}
			seen[link] = struct{}{}
			out = append(out, link)
		}
	}
	return out
}

type mieruLinkSettings struct {
	ShareLinkFormat string `json:"shareLinkFormat"`
	Clients         []struct {
		Email string `json:"email"`
	} `json:"clients"`
}

// mieruShareLinkFormat resolves the format for the generated native/simple
// pair from the Mieru inbound that owns the link user. Client e-mail is unique
// across panel records, so it is a stable identity even when client data was
// normalized into the clients table. Missing/unknown settings intentionally
// fall back to the interoperable Hiddify simple-sharing format.
func mieruShareLinkFormat(simpleLink string) string {
	u, err := url.Parse(simpleLink)
	if err != nil || u.User == nil {
		return "hiddify"
	}
	email := strings.TrimSpace(u.User.Username())
	if email == "" {
		return "hiddify"
	}

	db := database.GetDB()
	if db == nil {
		return "hiddify"
	}
	var inbounds []model.Inbound
	if err := db.Where("protocol = ?", model.Mieru).Find(&inbounds).Error; err != nil {
		return "hiddify"
	}
	for i := range inbounds {
		var settings mieruLinkSettings
		if json.Unmarshal([]byte(inbounds[i].Settings), &settings) != nil {
			continue
		}
		for _, client := range settings.Clients {
			if client.Email != email {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(settings.ShareLinkFormat), "native") {
				return "native"
			}
			return "hiddify"
		}
	}
	return "hiddify"
}

// splitLinkLines normalizes one generated multi-link entry into the lines that
// can be placed in a generic multi-protocol subscription.
//
// Mieru is a special case. The generator keeps both the native full-profile
// mieru:// form and the interoperable simple-sharing mierus:// form internally.
// They describe the same endpoint. Generic subscription parsers such as
// Hiddify/ray2sing treat both schemes as simple URLs, so only one representation
// must leave the panel. Existing inbounds default to mierus://; an inbound with
// shareLinkFormat=native keeps the protobuf mieru:// representation instead.
func splitLinkLines(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	hasSimpleMieru := false
	hasNativeMieru := false
	simpleMieru := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "mierus://"):
			hasSimpleMieru = true
			if simpleMieru == "" {
				simpleMieru = part
			}
		case strings.HasPrefix(part, "mieru://"):
			hasNativeMieru = true
		}
	}
	preferNativeMieru :=
		hasSimpleMieru && hasNativeMieru && mieruShareLinkFormat(simpleMieru) == "native"

	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if hasSimpleMieru && hasNativeMieru {
			if preferNativeMieru && strings.HasPrefix(p, "mierus://") {
				continue
			}
			if !preferNativeMieru && strings.HasPrefix(p, "mieru://") {
				continue
			}
		}
		if _, duplicate := seen[p]; duplicate {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
