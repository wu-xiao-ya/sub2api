package admin

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const serverMonitorMaxResponse = 1 << 20

type serverMonitorProxy struct {
	base   *url.URL
	token  string
	client *http.Client
}

// Only a deployment-configured private collector is reachable; user input never selects a URL.
func newServerMonitorProxyFromEnv() *serverMonitorProxy {
	return newServerMonitorProxy(os.Getenv("SERVER_MONITOR_URL"), os.Getenv("SERVER_MONITOR_TOKEN"))
}

func newServerMonitorProxy(rawURL, token string) *serverMonitorProxy {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && host != "host.docker.internal" && (ip == nil || (!ip.IsLoopback() && !ip.IsPrivate())) {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &serverMonitorProxy{base: u, token: token, client: &http.Client{
		Timeout: 8 * time.Second, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (h *SystemHandler) GetServerMonitorSummary(c *gin.Context) {
	h.proxyServerMonitor(c, "/api/summary", nil)
}

func (h *SystemHandler) GetServerMonitorLogs(c *gin.Context) {
	service := c.DefaultQuery("service", "sub2api")
	switch service {
	case "sub2api", "postgres", "redis", "caddy", "server-monitor":
	default:
		response.Error(c, http.StatusBadRequest, "Unsupported log service")
		return
	}
	lines, err := strconv.Atoi(c.DefaultQuery("lines", "80"))
	if err != nil || lines < 20 || lines > 200 {
		response.Error(c, http.StatusBadRequest, "Log lines must be between 20 and 200")
		return
	}
	h.proxyServerMonitor(c, "/api/logs", url.Values{"service": {service}, "lines": {strconv.Itoa(lines)}})
}

func (h *SystemHandler) proxyServerMonitor(c *gin.Context, path string, query url.Values) {
	c.Header("Cache-Control", "no-store")
	p := h.monitor
	if p == nil {
		response.Error(c, http.StatusServiceUnavailable, "Server monitor collector is not configured")
		return
	}
	u := *p.base
	u.Path, u.RawQuery = path, query.Encode()
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		response.Error(c, http.StatusBadGateway, "Server monitor request failed")
		return
	}
	req.Header.Set("X-Monitor-Token", p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		response.Error(c, http.StatusBadGateway, "Server monitor collector is unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		response.Error(c, http.StatusBadGateway, "Server monitor collector returned an error")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, serverMonitorMaxResponse+1))
	var value map[string]json.RawMessage
	if err != nil || len(body) > serverMonitorMaxResponse || json.Unmarshal(body, &value) != nil || value == nil {
		response.Error(c, http.StatusBadGateway, "Invalid server monitor response")
		return
	}
	response.Success(c, value)
}
