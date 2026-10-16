package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLeaderboardRangeCalendarAndDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	now := time.Date(2026, 3, 8, 14, 0, 0, 0, location)
	for _, tc := range []struct{ period, start string }{
		{"today", "2026-03-08"}, {"week", "2026-03-02"}, {"month", "2026-03-01"},
	} {
		start, end := leaderboardRange(tc.period, now)
		require.Equal(t, tc.start, start.Format("2006-01-02"))
		require.Equal(t, "2026-03-09", end.Format("2006-01-02"))
		require.Equal(t, 0, start.Hour())
		require.Equal(t, "2026-03-08", end.AddDate(0, 0, -1).Format("2006-01-02"))
	}
	start, end := leaderboardRange("today", now)
	require.Equal(t, 23*time.Hour, end.Sub(start))
	start, _ = leaderboardRange("all", now)
	require.Equal(t, int64(0), start.Unix())
}

func TestLeaderboardHandlerAuthenticationAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query string
		auth  bool
		code  int
	}{
		{"", false, http.StatusUnauthorized},
		{"?period=unknown", true, http.StatusBadRequest},
		{"?limit=invalid", true, http.StatusBadRequest},
		{"?limit=0", true, http.StatusBadRequest},
		{"?limit=101", true, http.StatusBadRequest},
	} {
		router := gin.New()
		if tc.auth {
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			})
		}
		router.GET("/leaderboard", NewLeaderboardHandler(nil).GetLeaderboard)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/leaderboard"+tc.query, nil))
		require.Equal(t, tc.code, response.Code, tc.query)
	}
}

func TestLeaderboardHandlerCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("(?s)SELECT.*FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "username", "tokens", "requests", "cost"}).
			AddRow(42, "person@example.test", nil, 1000, 3, 0.01))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	})
	router.GET("/leaderboard", NewLeaderboardHandler(service.NewLeaderboardService(db)).GetLeaderboard)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/leaderboard?period=week&timezone=Asia%2FTokyo", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Data struct {
			Entries []service.LeaderboardEntry `json:"leaderboard"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Len(t, payload.Data.Entries, 1)
	require.True(t, payload.Data.Entries[0].IsCurrentUser)
	require.NoError(t, mock.ExpectationsWereMet())
}
