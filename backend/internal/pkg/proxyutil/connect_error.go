package proxyutil

import "fmt"

// ConnectError identifies failures before a proxy tunnel was established.
// StatusCode is set only when the proxy returned an HTTP CONNECT response.
type ConnectError struct {
	ProxyHost  string
	Stage      string
	StatusCode int
	Err        error
}

func (e *ConnectError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("proxy CONNECT failed: status %d", e.StatusCode)
	}
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e *ConnectError) Unwrap() error { return e.Err }
