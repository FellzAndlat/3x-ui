package service

import (
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
)

type SubLinkProvider interface {
	SubLinksForSubId(host, subId string) ([]string, error)
	LinksForClient(host string, inbound *model.Inbound, email string) []string
	LinksForInbounds(host string, inbounds []*model.Inbound) []string
}

var registeredSubLinkProvider SubLinkProvider

func RegisterSubLinkProvider(p SubLinkProvider) {
	registeredSubLinkProvider = p
}

func (s *InboundService) GetSubLinks(host, subId string) ([]string, error) {
	if registeredSubLinkProvider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	return registeredSubLinkProvider.SubLinksForSubId(host, subId)
}

func (s *InboundService) GetAllInboundLinks(host string, userId int) ([]string, error) {
	if registeredSubLinkProvider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	inbounds, err := s.GetInbounds(userId)
	if err != nil {
		return nil, err
	}
	return registeredSubLinkProvider.LinksForInbounds(host, inbounds), nil
}

func (s *InboundService) GetAllClientLinks(host string, email string) ([]string, error) {
	if email == "" {
		return nil, common.NewError("client email is required")
	}
	if registeredSubLinkProvider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	rec, err := s.clientService.GetRecordByEmail(nil, email)
	if err != nil {
		return nil, err
	}
	inboundIds, err := s.clientService.GetInboundIdsForRecord(rec.Id)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, ibId := range inboundIds {
		inbound, getErr := s.GetInbound(ibId)
		if getErr != nil {
			return nil, getErr
		}
		links = append(links, registeredSubLinkProvider.LinksForClient(host, inbound, email)...)
	}
	return links, nil
}

// GetScopedInboundLinks uses the subscription renderer so server certificate
// pins, node addresses and endpoint overrides match the actual subscription.
func (s *InboundService) GetScopedInboundLinks(host string, inbound *model.Inbound, email string) ([]string, error) {
	if registeredSubLinkProvider == nil {
		return nil, common.NewError("sub link provider not registered")
	}
	if email == "" {
		return nil, common.NewError("client email is required")
	}
	links := registeredSubLinkProvider.LinksForClient(host, inbound, email)
	if len(links) == 0 {
		return nil, common.NewError("no connection links available; check client, certificate and public endpoint")
	}
	return links, nil
}
