package sub

import (
 "net"
 "net/url"
 "strconv"
 "strings"
 "github.com/SawaMEN/3x-ui/v3/internal/database/model"
 "github.com/SawaMEN/3x-ui/v3/internal/snell"
)

// Raw links are a portable credential export; native sing-box JSON is the
// authoritative subscription format for the multi-user extension.
func (s *SubService) genSnellLink(ib *model.Inbound, email string) string {
 settings, err := snell.Parse(ib.Settings); if err != nil { return "" }
 clients, err := s.clientsForLinkExport(ib); if err != nil { return "" }
 var links []string
 for _, c := range clients {
  if c.Email != email || !c.Enable || c.Password == "" { continue }
  for _, ep := range s.shareEndpointsForInbound(ib) {
   q := url.Values{"version":{strconv.Itoa(settings.ClientVersion())}, "userkey":{c.Password}}
   if settings.Version == 6 { q.Set("mode", settings.Mode) } else { q.Set("obfs", settings.ObfsMode); if settings.ObfsHost != "" { q.Set("obfs-host", settings.ObfsHost) } }
   u := url.URL{Scheme:"snell", User:url.User(settings.PSK), Host:net.JoinHostPort(ep.Address,strconv.Itoa(ep.Port)), RawQuery:q.Encode(), Fragment:ib.Remark}
   links = append(links,u.String())
  }
 }
 return strings.Join(links,"\n")
}
