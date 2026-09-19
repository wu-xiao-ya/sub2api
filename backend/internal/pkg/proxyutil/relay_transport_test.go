package proxyutil

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayTransportStalledCONNECTLeavesFallbackBudget(t *testing.T) {
	closed := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(closed)
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	ConfigureRelayConnectBudget(transport, u)
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(WithRelayConnectHost(ctx, u.Host), http.MethodGet, "https://example.test", nil)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if resp != nil {
		resp.Body.Close()
		t.Fatal("unexpected response")
	}
	var connectErr *ConnectError
	if !errors.As(err, &connectErr) || connectErr.ProxyHost != u.Host {
		t.Fatalf("expected structured proxy error, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("relay exhausted request deadline")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("CONNECT socket was not closed")
	}
}

func TestRelayTransportKeepsStreamingAndReusesTunnel(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to origin")
		}
		w.(http.Flusher).Flush()
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("stream"))
	}))
	defer origin.Close()
	var tunnels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Error("missing authenticated CONNECT")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			t.Error(err)
			return
		}
		defer upstream.Close()
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		tunnels.Add(1)
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, rw)
			_ = upstream.Close()
			close(done)
		}()
		_, _ = io.Copy(conn, upstream)
		_ = conn.Close()
		<-done
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	u.User = url.UserPassword("user", "pass")
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	ConfigureRelayConnectBudget(transport, u)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for i := 0; i < 2; i++ {
		// A short parent deadline is captured before net/http detaches dial
		// cancellation. Streaming itself deliberately uses a longer context.
		parent, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		marked := WithRelayConnectHost(parent, u.Host)
		ctx, stop := context.WithTimeout(context.WithoutCancel(marked), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			stop()
			cancel()
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		stop()
		cancel()
		if err != nil || string(body) != "stream" {
			t.Fatalf("CONNECT deadline affected streaming: %q, %v", body, err)
		}
	}
	if tunnels.Load() != 1 {
		t.Fatalf("expected tunnel reuse, got %d", tunnels.Load())
	}
}

func TestRelayTransportUnmarkedPreservesProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unmarked request changed to %s", r.Method)
		}
		_, _ = w.Write([]byte("original proxy"))
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	ConfigureRelayConnectBudget(transport, u)
	defer transport.CloseIdleConnections()
	resp, err := (&http.Client{Transport: transport}).Get("http://example.test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "original proxy" {
		t.Fatalf("default proxy changed: %s", body)
	}
}
