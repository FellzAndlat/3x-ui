package sub

import (
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"strings"
)

func (s *SubService) genAdditionalVPNLink(ib *model.Inbound, email string) string {
	if _, ok := s.clientForLink(ib, email); !ok {
		return ""
	}
	inst, err := externalvpn.FromInbound(ib)
	if err != nil {
		return ""
	}
	var links []string
	for _, ep := range s.shareEndpointsForInbound(ib) {
		link, err := externalvpn.ExportAdditionalLink(inst, email, ep.Address, ep.Port, ib.Remark)
		if err == nil {
			links = append(links, link)
		}
	}
	return strings.Join(links, "\n")
}
