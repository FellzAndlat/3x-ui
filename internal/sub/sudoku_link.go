package sub

import (
	"encoding/base64"
	"strings"

	"github.com/goccy/go-json"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
)

func (s *SubService) genSudokuLink(inbound *model.Inbound, email string) string {
	client, ok := s.clientForLink(inbound, email)
	if !ok || !sudoku.ValidPrivateKey(client.SudokuPrivateKey) {
		return ""
	}
	settings := s.linkSettings(inbound)
	mask, _ := settings["httpmask"].(map[string]any)
	links := make([]string, 0)
	for _, endpoint := range s.shareEndpointsForInbound(inbound) {
		host := strings.Trim(strings.TrimSpace(endpoint.Address), "[]")
		if host == "" || endpoint.Port <= 0 || endpoint.Port > 65535 {
			continue
		}
		payload := map[string]any{"h": host, "p": endpoint.Port, "k": client.SudokuPrivateKey}
		for source, target := range map[string]string{
			"ascii": "a", "aead": "e", "customTable": "t", "customTables": "ts",
			"multiplex": "hx",
		} {
			if value, ok := settings[source]; ok {
				payload[target] = value
			}
		}
		if pure, ok := settings["enablePureDownlink"].(bool); ok && !pure {
			payload["x"] = true
		}
		for source, target := range map[string]string{
			"disable": "hd", "mode": "hm", "tls": "ht", "host": "hh", "pathRoot": "hy",
		} {
			if value, ok := mask[source]; ok {
				payload[target] = value
			}
		}
		if endpoint.ForceTls == "tls" || endpoint.ForceTls == "none" {
			payload["ht"] = endpoint.ForceTls == "tls"
		}
		if value, ok := mask["multiplex"]; ok && value != "" {
			payload["hx"] = value
		}
		if encoded, err := json.Marshal(payload); err == nil {
			links = append(links, "sudoku://"+base64.RawURLEncoding.EncodeToString(encoded))
		}
	}
	return strings.Join(links, "\n")
}
