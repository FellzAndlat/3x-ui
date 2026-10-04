package singbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// SelectorClient contacts only the local core, never a remote controller.
type SelectorClient struct {
	address, secret string
	client          *http.Client
}
type SelectorState struct {
	Now string   `json:"now"`
	All []string `json:"all"`
}

func NewSelectorClient(controller, secret string) (*SelectorClient, error) {
	address, err := clashControllerURL(controller)
	if err != nil {
		return nil, err
	}
	return &SelectorClient{address: address, secret: secret, client: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *SelectorClient) request(ctx context.Context, method, tag string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.address+"/proxies/"+url.PathEscape(tag), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local selector API unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("local selector API returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
func (c *SelectorClient) State(ctx context.Context, tag string) (*SelectorState, error) {
	data, err := c.request(ctx, http.MethodGet, tag, nil)
	if err != nil {
		return nil, err
	}
	var state SelectorState
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid selector response")
	}
	return &state, nil
}
func (c *SelectorClient) Select(ctx context.Context, tag, target string) (string, error) {
	state, err := c.State(ctx, tag)
	if err != nil {
		return "", err
	}
	if !slices.Contains(state.All, target) {
		return state.Now, fmt.Errorf("target absent from running selector; reload the core after subscription changes")
	}
	if state.Now == target {
		return state.Now, nil
	}
	data, _ := json.Marshal(map[string]string{"name": target})
	if _, err = c.request(ctx, http.MethodPut, tag, data); err != nil {
		return state.Now, err
	}
	verified, err := c.State(ctx, tag)
	if err != nil {
		return "", err
	}
	if verified.Now != target {
		return verified.Now, fmt.Errorf("core did not confirm selected outbound")
	}
	return verified.Now, nil
}
func (c *SelectorClient) Close() { c.client.CloseIdleConnections() }
