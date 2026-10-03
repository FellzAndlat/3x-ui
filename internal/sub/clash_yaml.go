package sub

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

type yamlQuotedString string

func (s yamlQuotedString) MarshalYAML() ([]byte, error) {
	return []byte("'" + strings.ReplaceAll(string(s), "'", "''") + "'"), nil
}

var yamlNonStringWords = map[string]bool{
	"~": true, "null": true, "Null": true, "NULL": true,
	"true": true, "True": true, "TRUE": true,
	"false": true, "False": true, "FALSE": true,
	"yes": true, "Yes": true, "YES": true,
	"no": true, "No": true, "NO": true,
	"on": true, "On": true, "ON": true,
	"off": true, "Off": true, "OFF": true,
	"y": true, "Y": true, "n": true, "N": true,
}

var yamlNonStringNumber = regexp.MustCompile(`^(?:` +
	`[-+]?[0-9]+|` +
	`0[oO]?[0-7]+|0[xX][0-9a-fA-F]+|` +
	`[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+|` +
	`[-+]?(?:[0-9]*\.[0-9]+|[0-9]+\.?[0-9]*)(?:[eE][-+]?[0-9]+)?|` +
	`[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN)|` +
	`[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:[Tt ].*)?` +
	`)$`)

func yamlScalarIsAmbiguous(s string) bool {
	if s == "" {
		return false
	}
	return yamlNonStringWords[s] || yamlNonStringNumber.MatchString(s)
}

// normalizeClashCompatibility rewrites legacy/internal proxy field names at
// the serialization boundary. Keeping this here makes every Clash/Mihomo
// subscription path consistent without changing the in-memory builders used
// by other formats.
func normalizeClashCompatibility(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = normalizeClashCompatibility(item)
		}
		if proxyType, _ := out["type"].(string); strings.EqualFold(strings.TrimSpace(proxyType), "sudoku") {
			normalizeSudokuMihomoProxy(out)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = normalizeClashCompatibility(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			normalized, _ := normalizeClashCompatibility(item).(map[string]any)
			out = append(out, normalized)
		}
		return out
	default:
		return v
	}
}

func moveClashField(proxy map[string]any, from, to string) {
	value, ok := proxy[from]
	if !ok {
		return
	}
	if _, exists := proxy[to]; !exists {
		proxy[to] = value
	}
	delete(proxy, from)
}

func normalizeSudokuMihomoProxy(proxy map[string]any) {
	moveClashField(proxy, "aead", "aead-method")
	moveClashField(proxy, "ascii", "table-type")

	if tableType, ok := proxy["table-type"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(tableType)) {
		case "ascii":
			proxy["table-type"] = "prefer_ascii"
		case "entropy":
			proxy["table-type"] = "prefer_entropy"
		}
	}

	var httpMask map[string]any
	if current, ok := proxy["httpmask"].(map[string]any); ok {
		httpMask = current
	}
	if legacy, ok := proxy["http-mask"].(map[string]any); ok {
		if httpMask == nil {
			httpMask = legacy
		} else {
			for key, value := range legacy {
				if _, exists := httpMask[key]; !exists {
					httpMask[key] = value
				}
			}
		}
		delete(proxy, "http-mask")
	}
	if httpMask != nil {
		if pathRoot, ok := httpMask["pathRoot"]; ok {
			if _, exists := httpMask["path-root"]; !exists {
				httpMask["path-root"] = pathRoot
			}
			delete(httpMask, "pathRoot")
		}
		proxy["httpmask"] = httpMask
	}

	// Host endpoint forceTls is currently exposed by the Sudoku builder as a
	// top-level `tls` field. Mihomo reads TLS only from the nested httpmask
	// options, so move the override there instead of silently dropping it.
	if tls, ok := proxy["tls"].(bool); ok {
		if httpMask == nil {
			httpMask = make(map[string]any)
			proxy["httpmask"] = httpMask
		}
		httpMask["tls"] = tls
		delete(proxy, "tls")
	}
}

func quoteAmbiguousYAMLScalars(v any) any {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		if yamlScalarIsAmbiguous(s) {
			return yamlQuotedString(s)
		}
		return s
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key, ok := iter.Key().Interface().(string)
			if !ok {
				return v
			}
			out[key] = quoteAmbiguousYAMLScalars(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = quoteAmbiguousYAMLScalars(rv.Index(i).Interface())
		}
		return out
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return v
		}
		return quoteAmbiguousYAMLScalars(rv.Elem().Interface())
	default:
		return v
	}
}

func marshalClashYAML(config any) ([]byte, error) {
	return yaml.Marshal(quoteAmbiguousYAMLScalars(normalizeClashCompatibility(config)))
}
