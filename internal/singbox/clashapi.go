package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const clashAPIAddress = "http://127.0.0.1:10090"

type ClashConnection struct {
	ID       string        `json:"id"`
	Upload   int64         `json:"upload"`
	Download int64         `json:"download"`
	Start    time.Time     `json:"start"`
	Chains   []string      `json:"chains"`
	Metadata ClashMetadata `json:"metadata"`
}

type ClashMetadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	DNSMode         string `json:"dnsMode"`
	ProcessPath     string `json:"processPath"`
	User            string `json:"user"`
}

type clashConnectionsResponse struct {
	Connections   []ClashConnection `json:"connections"`
	UploadTotal   int64             `json:"uploadTotal"`
	DownloadTotal int64             `json:"downloadTotal"`
}

// ClashProxyDelay is the millisecond latency returned by sing-box's Clash API.
// Newer sing-box builds may also report delay2 for the second stage of a probe.
type ClashProxyDelay struct {
	Delay  int64 `json:"delay"`
	Delay2 int64 `json:"delay2,omitempty"`
}

type clashAPIError struct {
	Message string `json:"message"`
}

type ClashStatsClient struct {
	client  *http.Client
	baseURL string
	secret  string
}

func NewClashStatsClient() *ClashStatsClient {
	baseURL, secret := clashAPIInfoFromConfig()
	return &ClashStatsClient{
		client:  &http.Client{Timeout: 2 * time.Second},
		baseURL: baseURL,
		secret:  secret,
	}
}

func clashAPIInfoFromConfig() (string, string) {
	data, err := os.ReadFile(GetConfigPath())
	if err != nil {
		return clashAPIAddress, ""
	}
	var cfg struct {
		Experimental struct {
			ClashAPI struct {
				ExternalController string `json:"external_controller"`
				Secret             string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return clashAPIAddress, ""
	}
	return clashControllerURL(cfg.Experimental.ClashAPI.ExternalController), cfg.Experimental.ClashAPI.Secret
}

// clashControllerURL converts a sing-box bind address into a local HTTP URL.
// The controller is a local panel dependency; even when the config binds it to
// 0.0.0.0/[::], panel requests must stay on loopback instead of leaving the host.
func clashControllerURL(controller string) string {
	controller = strings.TrimSpace(controller)
	if controller == "" {
		return clashAPIAddress
	}
	if strings.HasPrefix(controller, "http://") || strings.HasPrefix(controller, "https://") {
		if parsed, err := url.Parse(controller); err == nil {
			controller = parsed.Host
		}
	}
	host, port, err := net.SplitHostPort(controller)
	if err != nil {
		// sing-box also accepts the common shorthand ":9090".
		if strings.HasPrefix(controller, ":") {
			port = strings.TrimPrefix(controller, ":")
		} else {
			return clashAPIAddress
		}
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil || port == "0" {
		return clashAPIAddress
	}
	if host != "" {
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			// external_controller is a bind address, not a remote endpoint. Never
			// send the configured Clash secret to a non-loopback destination.
			return "http://127.0.0.1:" + port
		}
	}
	return "http://127.0.0.1:" + port
}

func (c *ClashStatsClient) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	baseURL := strings.TrimRight(c.baseURL, "/")
	if baseURL == "" {
		baseURL = clashAPIAddress
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return req, nil
}

func (c *ClashStatsClient) Connections(ctx context.Context) ([]ClashConnection, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/connections")
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sing-box Clash API returned HTTP %d", resp.StatusCode)
	}
	var payload clashConnectionsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Connections, nil
}

// ProxyDelay performs an on-demand URL test through one running sing-box
// outbound. The timeout is passed to sing-box and also enforced client-side so
// a broken proxy cannot leave a panel request blocked indefinitely.
func (c *ClashStatsClient) ProxyDelay(ctx context.Context, tag, testURL string, timeout time.Duration) (*ClashProxyDelay, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, fmt.Errorf("outbound tag is required")
	}
	testURL = strings.TrimSpace(testURL)
	parsed, err := url.Parse(testURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("test URL must be an absolute HTTP or HTTPS URL")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}

	query := url.Values{}
	query.Set("url", testURL)
	query.Set("timeout", strconv.FormatInt(max(1, timeout.Milliseconds()), 10))
	endpoint := "/proxies/" + url.PathEscape(tag) + "/delay?" + query.Encode()

	requestCtx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	req, err := c.newRequest(requestCtx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}

	probeClient := *c.client
	probeClient.Timeout = timeout + time.Second
	resp, err := probeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sing-box outbound %q probe failed: %w", tag, err)
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, 64<<10)
	if resp.StatusCode != http.StatusOK {
		var apiErr clashAPIError
		_ = json.NewDecoder(body).Decode(&apiErr)
		if apiErr.Message != "" {
			return nil, fmt.Errorf("sing-box outbound %q probe failed: %s", tag, apiErr.Message)
		}
		return nil, fmt.Errorf("sing-box outbound %q probe failed: HTTP %d", tag, resp.StatusCode)
	}

	var result ClashProxyDelay
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode sing-box outbound %q probe: %w", tag, err)
	}
	return &result, nil
}

func (c *ClashStatsClient) UserEmails(ctx context.Context) ([]string, error) {
	connections, err := c.Connections(ctx)
	if err != nil {
		return nil, err
	}
	users := make(map[string]struct{})
	for _, connection := range connections {
		if connection.Metadata.User != "" {
			users[connection.Metadata.User] = struct{}{}
		}
	}
	result := make([]string, 0, len(users))
	for user := range users {
		result = append(result, user)
	}
	sort.Strings(result)
	return result, nil
}

func (c *ClashStatsClient) OnlineIPSet(ctx context.Context) (map[string]map[string]struct{}, int, error) {
	connections, err := c.Connections(ctx)
	if err != nil {
		return nil, 0, err
	}
	byInbound := make(map[string]map[string]struct{})
	for _, connection := range connections {
		ip := connection.Metadata.SourceIP
		if ip == "" {
			continue
		}
		inbound := connection.Metadata.Type
		if inbound == "" {
			inbound = "unknown"
		}
		if byInbound[inbound] == nil {
			byInbound[inbound] = make(map[string]struct{})
		}
		byInbound[inbound][ip] = struct{}{}
	}
	return byInbound, len(connections), nil
}
