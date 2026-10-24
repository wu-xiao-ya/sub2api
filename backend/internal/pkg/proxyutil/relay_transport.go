package proxyutil

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// ConfigureRelayConnectBudget must be called before the transport is used.
// Only requests explicitly marked for this HTTP relay use the bounded tunnel.
// Other requests retain the transport's existing proxy and dial behavior.
func ConfigureRelayConnectBudget(transport *http.Transport, proxyURL *url.URL) {
	if proxyURL == nil || proxyURL.Scheme != "http" {
		return
	}
	proxy := *proxyURL
	originalProxy := transport.Proxy
	originalDial := transport.DialContext
	if originalDial == nil {
		originalDial = (&net.Dialer{}).DialContext
		// Adding a custom dial hook otherwise disables net/http's implicit H2.
		// Preserve explicit H1/TLS configuration and the original default mode.
		if transport.DialTLSContext == nil && transport.DialTLS == nil &&
			transport.TLSClientConfig == nil && transport.TLSNextProto == nil {
			transport.ForceAttemptHTTP2 = true
		}
	}
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if isRelayConnectHost(req.Context(), proxy.Host) {
			// DialContext establishes the tunnel; net/http still owns origin TLS.
			return nil, nil
		}
		if originalProxy != nil {
			return originalProxy(req)
		}
		return nil, nil
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !isRelayConnectHost(ctx, proxy.Host) {
			return originalDial(ctx, network, addr)
		}
		connectCtx, cancel := RelayConnectContext(ctx, proxy.Host)
		defer cancel()
		proxyAddr := proxy.Host
		if proxy.Port() == "" {
			proxyAddr = net.JoinHostPort(proxy.Hostname(), "80")
		}
		conn, err := originalDial(connectCtx, network, proxyAddr)
		if err != nil {
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "connect to proxy", Err: err}
		}
		ok := false
		defer func() {
			if !ok {
				_ = conn.Close()
			}
		}()
		if deadline, exists := connectCtx.Deadline(); exists {
			_ = conn.SetDeadline(deadline)
		}
		closed := make(chan struct{})
		stopClose := context.AfterFunc(connectCtx, func() {
			_ = conn.Close()
			close(closed)
		})
		defer stopClose()
		req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: make(http.Header)}
		if proxy.User != nil {
			password, _ := proxy.User.Password()
			req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
		}
		if err := req.Write(conn); err != nil {
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "write CONNECT request", Err: err}
		}
		reader := bufio.NewReader(conn)
		resp, err := http.ReadResponse(reader, req)
		if err != nil {
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "read CONNECT response", Err: err}
		}
		if resp.StatusCode != http.StatusOK {
			return nil, &ConnectError{ProxyHost: proxy.Host, StatusCode: resp.StatusCode}
		}
		// Detach cancellation before returning the socket to the streaming client.
		if !stopClose() {
			<-closed
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "proxy CONNECT cancelled", Err: connectCtx.Err()}
		}
		if err := connectCtx.Err(); err != nil {
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "proxy CONNECT cancelled", Err: err}
		}
		if err := conn.SetDeadline(time.Time{}); err != nil {
			return nil, &ConnectError{ProxyHost: proxy.Host, Stage: "clear CONNECT deadline", Err: err}
		}
		ok = true
		return &bufferedTunnelConn{Conn: conn, reader: reader}, nil
	}
}

type bufferedTunnelConn struct {
	net.Conn
	reader io.Reader
}

func (c *bufferedTunnelConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
