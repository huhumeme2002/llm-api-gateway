package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
	"llmgw/internal/protocol"
)

type HTTPConfig struct {
	Name      string
	BaseURL   string
	APIKey    string
	Timeout   time.Duration
	Extra     map[string]string
	ProxyURL  string // dedicated outbound proxy for this client only
	CredID    string
}

type HTTPClient struct {
	cfg    HTTPConfig
	client *http.Client
}

func NewHTTP(cfg HTTPConfig) *HTTPClient {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Minute
	}
	if cfg.CredID == "" {
		cfg.CredID = "default"
	}
	tr, err := newTransport(cfg.ProxyURL)
	if err != nil {
		// Keep a dedicated (broken) client so we never fall back to a shared
		// direct transport. Calls will fail until the proxy URL is fixed.
		tr = &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return nil, RedactError(err)
			},
		}
	}
	return &HTTPClient{
		cfg:    cfg,
		client: &http.Client{Transport: tr},
	}
}

func newTransport(proxyRaw string) (*http.Transport, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		// Never ProxyFromEnvironment — each credential owns its route.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		MaxConnsPerHost:       24,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	proxyRaw = strings.TrimSpace(proxyRaw)
	if proxyRaw == "" {
		return tr, nil
	}
	u, err := url.Parse(proxyRaw)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h", "socks":
		var auth *xproxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
		}
		sd, err := xproxy.SOCKS5("tcp", u.Host, auth, dialer)
		if err != nil {
			return nil, err
		}
		if cd, ok := sd.(xproxy.ContextDialer); ok {
			tr.DialContext = cd.DialContext
		} else {
			tr.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
				return sd.Dial(network, addr)
			}
		}
		tr.Proxy = nil
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	return tr, nil
}

func (c *HTTPClient) Name() string   { return c.cfg.Name }
func (c *HTTPClient) CredID() string { return c.cfg.CredID }
func (c *HTTPClient) HasProxy() bool { return strings.TrimSpace(c.cfg.ProxyURL) != "" }
func (c *HTTPClient) ProxyKind() string {
	return ProxyKind(c.cfg.ProxyURL)
}
func (c *HTTPClient) Transport() http.RoundTripper { return c.client.Transport }

func (c *HTTPClient) GetJSON(ctx context.Context, path string) ([]byte, int, http.Header, error) {
	return c.do(ctx, http.MethodGet, path, nil, false)
}

type cancelCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.cancel != nil {
		c.cancel()
	}
	return err
}

func (c *HTTPClient) PostJSON(ctx context.Context, path string, body []byte, stream bool) ([]byte, int, http.Header, io.ReadCloser, error) {
	ctx, cancel := withProviderTimeout(ctx, c.cfg.Timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, join(c.cfg.BaseURL, path), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, 0, nil, nil, err
	}
	c.auth(req)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	for k, v := range c.cfg.Extra {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		cancel()
		return nil, 0, nil, nil, RedactError(err)
	}
	if stream && resp.StatusCode < 300 {
		return nil, resp.StatusCode, resp.Header.Clone(), &cancelCloser{ReadCloser: resp.Body, cancel: cancel}, nil
	}
	defer cancel()
	defer Drain(resp.Body)
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Clone(), nil, err
	}
	if resp.StatusCode >= 300 {
		return b, resp.StatusCode, resp.Header.Clone(), nil, fmt.Errorf("upstream %s: %s", resp.Status, truncate(b, 400))
	}
	return b, resp.StatusCode, resp.Header.Clone(), nil, nil
}

func withProviderTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok || d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func (c *HTTPClient) do(ctx context.Context, method, path string, body []byte, stream bool) ([]byte, int, http.Header, error) {
	ctx, cancel := withProviderTimeout(ctx, 20*time.Second)
	defer cancel()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, join(c.cfg.BaseURL, path), rdr)
	if err != nil {
		return nil, 0, nil, err
	}
	c.auth(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.cfg.Extra {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, nil, RedactError(err)
	}
	defer Drain(resp.Body)
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Clone(), err
	}
	if resp.StatusCode >= 300 {
		return b, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("upstream %s: %s", resp.Status, truncate(b, 400))
	}
	return b, resp.StatusCode, resp.Header.Clone(), nil
}

func (c *HTTPClient) auth(req *http.Request) {
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
}

func join(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func StreamBody(ctx context.Context, body io.ReadCloser, proto protocol.Protocol, on StreamHandler) (protocol.Completion, error) {
	defer Drain(body)
	return protocol.ConsumeSSE(body, proto, func(ev protocol.StreamEvent) error {
		if on == nil {
			return nil
		}
		return on(ev)
	})
}
