package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func TestRelayTLSFingerprintStalledConnect(t *testing.T) {
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
	parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx := proxyutil.WithRelayConnectHost(parent, u.Host)
	conn, err := tlsfingerprint.NewHTTPProxyDialer(nil, u).DialTLSContext(ctx, "tcp", "example.test:443")
	if conn != nil {
		conn.Close()
		t.Fatal("unexpected tunnel")
	}
	if !IsRelayConnectError(err, u.Host) {
		t.Fatalf("stalled CONNECT not classified as relay failure: %v", err)
	}
	if parent.Err() != nil {
		t.Fatal("no request time remains for fallback")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("stalled CONNECT socket leaked")
	}
}

func TestRelayTLSFingerprintOriginHandshakeUsesParent(t *testing.T) {
	closed := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(closed)
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(io.Discard, conn)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx := proxyutil.WithRelayConnectHost(parent, u.Host)
	conn, err := tlsfingerprint.NewHTTPProxyDialer(nil, u).DialTLSContext(ctx, "tcp", "example.test:443")
	if conn != nil {
		conn.Close()
		t.Fatal("unexpected TLS session")
	}
	if parent.Err() == nil {
		t.Fatalf("origin TLS terminated before parent deadline: %v", err)
	}
	if IsRelayConnectError(err, u.Host) {
		t.Fatalf("origin TLS incorrectly trips relay cooldown: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("origin TLS socket leaked")
	}
}

func TestRelayTLSFingerprintConnectErrors(t *testing.T) {
	for _, status := range []int{403, 407, 502, 503, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer proxy.Close()
			u, _ := url.Parse(proxy.URL)
			dialer := tlsfingerprint.NewHTTPProxyDialer(nil, u)
			conn, err := dialer.DialTLSContext(context.Background(), "tcp", "example.test:443")
			if conn != nil {
				_ = conn.Close()
				t.Fatal("unexpected tunnel")
			}
			var connectErr *proxyutil.ConnectError
			if !errors.As(err, &connectErr) || connectErr.StatusCode != status {
				t.Fatalf("missing structured CONNECT error: %v", err)
			}
			if want := status == 403 || status == 407; IsRelayConnectError(err, u.Host) != want {
				t.Fatalf("incorrect relay classification for %d", status)
			}
		})
	}
}

func TestRelayTLSFingerprintHostIsolation(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &proxyutil.ConnectError{ProxyHost: "other:38480", Stage: "read CONNECT response", Err: io.EOF})
	if IsRelayConnectError(err, "relay:38480") {
		t.Fatal("another proxy must not trip this relay")
	}
	if !IsRelayConnectError(err, "other:38480") {
		t.Fatal("CONNECT EOF was not classified")
	}
	if IsRelayConnectError(errors.New("TLS handshake failed: certificate verification failed"), "relay:38480") {
		t.Fatal("origin TLS failure must not trip relay")
	}
}
