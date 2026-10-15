package admin

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseDashboardExcludedUsersAcceptsIDsAndEmails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/dashboard/snapshot-v2?exclude_users=42%2CBlocked%40Example.com%3B42", nil)

	ids, emails, err := parseDashboardExcludedUsers(c)

	require.NoError(t, err)
	require.Equal(t, []int64{42}, ids)
	require.Equal(t, []string{"blocked@example.com"}, emails)
}

func TestParseDashboardExcludedUsersRejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/dashboard/snapshot-v2?exclude_users=not-an-id", nil)

	_, _, err := parseDashboardExcludedUsers(c)

	require.EqualError(t, err, "excluded users must be numeric IDs or email addresses")
}
