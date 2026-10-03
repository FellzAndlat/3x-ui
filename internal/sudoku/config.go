package sudoku

type HTTPMaskConfig struct {
	Disable   bool   `json:"disable"`
	Mode      string `json:"mode"`
	TLS       bool   `json:"tls"`
	Host      string `json:"host"`
	PathRoot  string `json:"path_root"`
	Multiplex string `json:"multiplex"`
}

type Config struct {
	Mode               string         `json:"mode"`
	Transport          string         `json:"transport"`
	LocalPort          int            `json:"local_port"`
	ServerAddress      string         `json:"server_address,omitempty"`
	FallbackAddr       string         `json:"fallback_address"`
	Key                string         `json:"key"`
	AEAD               string         `json:"aead"`
	SuspiciousAction   string         `json:"suspicious_action"`
	PaddingMin         int            `json:"padding_min"`
	PaddingMax         int            `json:"padding_max"`
	ASCII              string         `json:"ascii"`
	CustomTable        string         `json:"custom_table"`
	CustomTables       []string       `json:"custom_tables"`
	EnablePureDownlink bool           `json:"enable_pure_downlink"`
	Multiplex          string         `json:"multiplex"`
	HTTPMask           HTTPMaskConfig `json:"httpmask"`
}
