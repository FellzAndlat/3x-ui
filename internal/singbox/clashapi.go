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
	client    *http.Client
	baseURL   string
	secret    string
	configErr error
}

func NewClashStatsClient() *ClashStatsClient {
	baseURL, secret, err := clashAPIInfoFromConfig()
	return &ClashStatsClient{
		client:    &http.Client{Timeout: 2 * time.Second},
		baseURL:   baseURL,
		secret:    secret,
		configErr: err,
	}
}

func clashAPIInfoFromConfig() (string, string, error) {
	data, err := os.ReadFile(GetConfigPath())
	if err != nil {
		return "", "", fmt.Errorf("read sing-box config: %w", err)
	}
	return clashAPIInfo(data)
}

// clashAPIInfo derives the controller endpoint from the effective runtime
// configuration. No implicit fallback is used here: according to sing-box,
// an omitted or empty external_controller means the Clash API is disabled.
func clashAPIInfo(data []byte) (string, string, error) {
	type clashAPIConfig struct {
		ExternalController *string `json:"external_controller"`
		Secret             string  `json:"secret"`
	}
	var cfg struct {
		Experimental struct {
			ClashAPI *clashAPIConfig `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", fmt.Errorf("parse sing-box config: %w", err)
	}
	if cfg.Experimental.ClashAPI == nil {
		return "", "", nil
	}

	secret := cfg.Experimental.ClashAPI.Secret
	if cfg.Experimental.ClashAPI.ExternalController == nil {
		return "", secret, nil
	}
	controller := strings.TrimSpace(*cfg.Experimental.ClashAPI.ExternalController)
	if controller == "" {
		return "", secret, nil
	}
	baseURL, err := clashControllerURL(controller)
	if err != nil {
		return "", "", err
	}
	return baseURL, secret, nil
}

// clashControllerURL converts a sing-box external_controller bind address into
// the endpoint the panel can safely call. Wildcard binds are mapped to the
// matching loopback family. Explicit non-loopback addresses are allowed only
// when that address is assigned to this host, so a Clash secret can never be
// sent to an arbitrary remote endpoint.
func clashControllerURL(controller string) (string, error) {
	controller = strings.TrimSpace(controller)
	if controller == "" {
		return "", fmt.Errorf("sing-box Clash API is disabled")
	}
	if strings.Contains(controller, "://") {
		return "", fmt.Errorf("experimental.clash_api.external_controller must be a bind address, not a URL")
	}

	host, port, err := net.SplitHostPort(controller)
	if err != nil {
		return "", fmt.Errorf("invalid sing-box Clash API controller %q: %w", controller, err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return "", fmt.Errorf("invalid sing-box Clash API controller port %q", port)
	}

	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	} else if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	} else {
		plainHost := strings.Trim(host, "[]")
		ip := net.ParseIP(plainHost)
		if ip == nil {
			return "", fmt.Errorf("sing-box Clash API controller host %q must be an IP address or localhost", host)
		}
		switch {
		case ip.IsUnspecified() && ip.To4() != nil:
			host = "127.0.0.1"
		case ip.IsUnspecified():
			host = "::1"
		case ip.IsLoopback():
			host = ip.String()
		default:
			local, err := isLocalIP(ip)
			if err != nil {
				return "", fmt.Errorf("inspect local interfaces for Clash API controller: %w", err)
			}
			if !local {
				return "", fmt.Errorf("sing-box Clash API controller address %q is not assigned to this host", host)
			}
			host = ip.String()
		}
	}

	return "http://" + net.JoinHostPort(host, port), nil
}

func isLocalIP(target net.IP) (bool, error) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false, err
	}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		default:
			continue
		}
		if ip.Equal(target) {
			return true, nil
		}
	}
	return false, nil
}

func (c *ClashStatsClient) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	if c == nil {
		return nil, fmt.Errorf("sing-box Clash API client is nil")
	}
	if c.configErr != nil {
		return nil, fmt.Errorf("sing-box Clash API configuration: %w", c.configErr)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("sing-box Clash API is disabled")
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

func (c *ClashStatsClient) requestClient(timeout time.Duration) *http.Client {
	client := http.Client{}
	if c != nil && c.client != nil {
		client = *c.client
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func (c *ClashStatsClient) Connections(ctx context.Context) ([]ClashConnection, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/connections")
	if err != nil {
		return nil, err
	}
	resp, err := c.requestClient(2 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, 1<<20)
	if resp.StatusCode != http.StatusOK {
		var apiErr clashAPIError
		_ = json.NewDecoder(body).Decode(&apiErr)
		if apiErr.Message != "" {
			return nil, fmt.Errorf("sing-box Clash API returned HTTP %d: %s", resp.StatusCode, apiErr.Message)
		}
		return nil, fmt.Errorf("sing-box Clash API returned HTTP %d", resp.StatusCode)
	}
	var payload clashConnectionsResponse
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode sing-box Clash API connections: %w", err)
	}
	return payload.Connections, nil
}

// ProxyDelay performs an on-demand URL test through one running sing-box
// outbound. sing-box's Clash API currently ignores explicit http:// targets,
// so accepting them here would report latency for a different fallback URL.
func (c *ClashStatsClient) ProxyDelay(ctx context.Context, tag, testURL string, timeout time.Duration) (*ClashProxyDelay, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, fmt.Errorf("outbound tag is required")
	}
	testURL = strings.TrimSpace(testURL)
	parsed, err := url.Parse(testURL)
	if err != nil || parsed.Hostname() == "" || !strings.EqualFold(parsed.Scheme, "https") {
		return nil, fmt.Errorf("test URL must be an absolute HTTPS URL")
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

	resp, err := c.requestClient(timeout + time.Second).Do(req)
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
