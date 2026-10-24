package service

import (
	"bytes"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
)

// newAntigravityCompatContext builds a gin context carrying the given request
// body; the Gemini compat tests use it to drive handlers without a router.
func newAntigravityCompatContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	return c, recorder
}

// optionalStringValue dereferences an optional string field for assertions.
func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
