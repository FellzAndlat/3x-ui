package sub

import (
	"encoding/base64"
	"strings"

	"github.com/goccy/go-json"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/sudoku"
)

func sudokuLinkString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func sudokuLinkASCII(value any) string {
	mode := strings.ToLower(sudokuLinkString(value))
	switch mode {
	case "prefer_ascii", "ascii":
		return "ascii"
	case "", "prefer_entropy", "entropy":
		return "entropy"
	default:
		return mode
	}
}

func sudokuLinkAEAD(value any) string {
	method := strings.ToLower(sudokuLinkString(value))
	if method == "" {
		return "chacha20-poly1305"
	}
	return method
}

func sudokuLinkMux(settings map[string]any, mask map[string]any) string {
	if value := strings.ToLower(sudokuLinkString(mask["multiplex"])); value != "" {
		return value
	}
	return strings.ToLower(sudokuLinkString(settings["multiplex"]))
}

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

		// Keep the payload compatible with Sudoku's own short-link encoder. In
		// particular, an omitted AEAD decodes as "none" in upstream Sudoku, so
		// emit the canonical default explicitly instead of relying on omitempty.
		payload := map[string]any{
			"h": host,
			"p": endpoint.Port,
			"k": client.SudokuPrivateKey,
			"a": sudokuLinkASCII(settings["ascii"]),
			"e": sudokuLinkAEAD(settings["aead"]),
			"m": 1080,
		}
		if value := sudokuLinkString(settings["customTable"]); value != "" {
			payload["t"] = value
		}
		if values, ok := settings["customTables"].([]any); ok {
			clean := make([]string, 0, len(values))
			for _, raw := range values {
				if value := sudokuLinkString(raw); value != "" {
					clean = append(clean, value)
				}
			}
			if len(clean) > 0 {
				payload["ts"] = clean
			}
		} else if values, ok := settings["customTables"].([]string); ok {
			clean := make([]string, 0, len(values))
			for _, raw := range values {
				if value := strings.TrimSpace(raw); value != "" {
					clean = append(clean, value)
				}
			}
			if len(clean) > 0 {
				payload["ts"] = clean
			}
		}
		if pure, ok := settings["enablePureDownlink"].(bool); ok && !pure {
			payload["x"] = true
		}

		if disabled, ok := mask["disable"].(bool); ok && disabled {
			payload["hd"] = true
		}
		if mode := strings.ToLower(sudokuLinkString(mask["mode"])); mode != "" && mode != "legacy" {
			payload["hm"] = mode
		}
		tlsEnabled, _ := mask["tls"].(bool)
		switch endpoint.ForceTls {
		case "tls":
			tlsEnabled = true
		case "none":
			tlsEnabled = false
		}
		if tlsEnabled {
			payload["ht"] = true
		}
		if value := sudokuLinkString(mask["host"]); value != "" {
			payload["hh"] = value
		}
		if value := sudokuLinkString(mask["pathRoot"]); value != "" {
			payload["hy"] = value
		}
		if value := sudokuLinkMux(settings, mask); value != "" && value != "off" {
			payload["hx"] = value
		}

		if encoded, err := json.Marshal(payload); err == nil {
			links = append(links, "sudoku://"+base64.RawURLEncoding.EncodeToString(encoded))
		}
	}
	return strings.Join(links, "\n")
}
