package provider

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/things-go/go-socks5"
)

func startOrigin(t *testing.T, hits *atomic.Int32, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func startHTTPProxy(t *testing.T, user, pass string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			got := r.Header.Get("Proxy-Authorization")
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
			if got != want {
				w.Header().Set("Proxy-Authenticate", `Basic realm="p"`)
				w.WriteHeader(http.StatusProxyAuthRequired)
				return
			}
		}
		hits.Add(1)
		if r.Method == http.MethodConnect {
			dest, err := net.DialTimeout("tcp", r.Host, 3*time.Second)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", 500)
				return
			}
			w.WriteHeader(http.StatusOK)
			conn, _, err := hj.Hijack()
			if err != nil {
				_ = dest.Close()
				return
			}
			go func() { _, _ = io.Copy(dest, conn); _ = dest.Close(); _ = conn.Close() }()
			_, _ = io.Copy(conn, dest)
			_ = dest.Close()
			_ = conn.Close()
			return
		}
		out, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		out.Header = r.Header.Clone()
		out.Header.Del("Proxy-Authorization")
		resp, err := http.DefaultTransport.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
}

func TestHTTPProxyAndAuth(t *testing.T) {
	var originHits, proxyHits atomic.Int32
	origin := startOrigin(t, &originHits, `{"id":"1","choices":[{"message":{"content":"ok"}}]}`)
	defer origin.Close()
	px := startHTTPProxy(t, "alice", "wonder", &proxyHits)
	defer px.Close()

	cli := NewHTTP(HTTPConfig{
		BaseURL: origin.URL, APIKey: "sk-testkey12345678", Timeout: 5 * time.Second,
		ProxyURL: strings.Replace(px.URL, "http://", "http://alice:wonder@", 1),
		CredID:   "go-01",
	})
	b, status, _, err := cli.GetJSON(context.Background(), "models")
	if err != nil || status != 200 {
		t.Fatalf("status=%d err=%v body=%s", status, err, b)
	}
	if proxyHits.Load() == 0 || originHits.Load() == 0 {
		t.Fatalf("proxy=%d origin=%d", proxyHits.Load(), originHits.Load())
	}

	bad := NewHTTP(HTTPConfig{BaseURL: origin.URL, ProxyURL: strings.Replace(px.URL, "http://", "http://alice:wrong@", 1), CredID: "x"})
	_, st, _, err := bad.GetJSON(context.Background(), "models")
	if err == nil && st == 200 {
		t.Fatal("bad proxy auth must fail")
	}
}

func TestNoDirectFallbackWhenProxyDead(t *testing.T) {
	var originHits atomic.Int32
	origin := startOrigin(t, &originHits, `{}`)
	defer origin.Close()
	cli := NewHTTP(HTTPConfig{
		BaseURL: origin.URL, ProxyURL: "http://127.0.0.1:1", CredID: "go-01", Timeout: 2 * time.Second,
	})
	_, _, _, err := cli.GetJSON(context.Background(), "models")
	if err == nil {
		t.Fatal("expected proxy failure")
	}
	if originHits.Load() != 0 {
		t.Fatal("must not bypass dead proxy")
	}
	if strings.Contains(err.Error(), "http://127.0.0.1:1") && strings.Contains(err.Error(), "alice:") {
		t.Fatal("should not leak")
	}
}

func TestTwoKeysDifferentTransports(t *testing.T) {
	var o, p1, p2 atomic.Int32
	origin := startOrigin(t, &o, `{"ok":true}`)
	defer origin.Close()
	px1 := startHTTPProxy(t, "", "", &p1)
	defer px1.Close()
	px2 := startHTTPProxy(t, "", "", &p2)
	defer px2.Close()

	a := NewHTTP(HTTPConfig{BaseURL: origin.URL, ProxyURL: px1.URL, CredID: "a"})
	b := NewHTTP(HTTPConfig{BaseURL: origin.URL, ProxyURL: px2.URL, CredID: "b"})
	if a.Transport() == b.Transport() {
		t.Fatal("credentials must not share Transport")
	}
	_, _, _, err := a.GetJSON(context.Background(), "models")
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = b.GetJSON(context.Background(), "models")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Load() == 0 || p2.Load() == 0 {
		t.Fatalf("p1=%d p2=%d", p1.Load(), p2.Load())
	}
}

func TestStreamThroughHTTPProxy(t *testing.T) {
	var originHits, proxyHits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer origin.Close()
	px := startHTTPProxy(t, "", "", &proxyHits)
	defer px.Close()
	cli := NewHTTP(HTTPConfig{BaseURL: origin.URL, ProxyURL: px.URL, CredID: "go-01", Timeout: 5 * time.Second})
	_, status, _, rc, err := cli.PostJSON(context.Background(), "chat/completions", []byte(`{"stream":true}`), true)
	if err != nil || status != 200 {
		t.Fatalf("%d %v", status, err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !strings.Contains(string(b), "hi") {
		t.Fatal(string(b))
	}
	if proxyHits.Load() == 0 || originHits.Load() == 0 {
		t.Fatal("stream did not use proxy")
	}
}

func TestSOCKS5ProxyAuth(t *testing.T) {
	var originHits atomic.Int32
	origin := startOrigin(t, &originHits, `{"ok":true}`)
	defer origin.Close()

	cator := socks5.UserPassAuthenticator{Credentials: socks5.StaticCredentials{"bob": "builder"}}
	srv := socks5.NewServer(socks5.WithAuthMethods([]socks5.Authenticator{cator}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()

	cli := NewHTTP(HTTPConfig{
		BaseURL: origin.URL, Timeout: 5 * time.Second,
		ProxyURL: "socks5://bob:builder@" + ln.Addr().String(),
		CredID:   "s5",
	})
	_, status, _, err := cli.GetJSON(context.Background(), "models")
	if err != nil || status != 200 {
		t.Fatalf("%d %v", status, err)
	}
	if originHits.Load() == 0 {
		t.Fatal("origin not reached via socks5")
	}

	bad := NewHTTP(HTTPConfig{BaseURL: origin.URL, ProxyURL: "socks5://bob:wrong@" + ln.Addr().String(), Timeout: 2 * time.Second})
	_, _, _, err = bad.GetJSON(context.Background(), "models")
	if err == nil {
		t.Fatal("bad socks auth must fail")
	}
}
