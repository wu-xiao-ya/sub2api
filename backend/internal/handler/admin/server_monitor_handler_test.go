package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestServerMonitorConfigRejectsUnsafeTargets(t *testing.T) {
	for _, target := range []string{"", "https://example.com", "file:///etc/passwd", "http://127.0.0.1/private", "http://u:p@127.0.0.1", "http://127.0.0.1?url=other"} {
		require.Nil(t, newServerMonitorProxy(target, "secret"), target)
	}
	require.Nil(t, newServerMonitorProxy("http://127.0.0.1:18787", ""))
}

func TestServerMonitorProxy(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status, want int
	}{
		{"ok", `{"cpu":{"percent":10},"samples":[]}`, 200, 200},
		{"invalid", "not json", 200, 502},
		{"array", "[]", 200, 502},
		{"null", "null", 200, 502},
		{"oversized", strings.Repeat("x", serverMonitorMaxResponse+1), 200, 502},
		{"upstream error", "private diagnostic", 500, 502},
		{"redirect", "", 302, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/summary", r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				require.Equal(t, "collector-secret", r.Header.Get("X-Monitor-Token"))
				require.Empty(t, r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Cookie"))
				w.Header().Set("Location", "/redirect-target")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			h := &SystemHandler{monitor: newServerMonitorProxy(upstream.URL, "collector-secret")}
			r := gin.New()
			r.GET("/summary", h.GetServerMonitorSummary)
			req := httptest.NewRequest("GET", "/summary?url=http://other", nil)
			req.Header.Set("Authorization", "Bearer admin-secret")
			req.Header.Set("Cookie", "auth=secret")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.want, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.NotContains(t, w.Body.String(), "private diagnostic")
		})
	}
}

func TestServerMonitorLogValidationAndDisabled(t *testing.T) {
	h := &SystemHandler{}
	r := gin.New()
	r.GET("/logs", h.GetServerMonitorLogs)
	r.GET("/summary", h.GetServerMonitorSummary)
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"/logs?service=../../etc/passwd", 400}, {"/logs?lines=201", 400},
		{"/logs?lines=0", 400}, {"/logs?lines=x", 400},
		{"/logs?service=sub2api&lines=80", 503}, {"/summary", 503},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		require.Equal(t, tc.status, w.Code)
	}
}
