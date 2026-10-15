package user

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// LeaderboardHandler handles user leaderboard endpoints
type LeaderboardHandler struct {
	leaderboardService *service.LeaderboardService
}

// NewLeaderboardHandler creates a new leaderboard handler
func NewLeaderboardHandler(leaderboardService *service.LeaderboardService) *LeaderboardHandler {
	return &LeaderboardHandler{
		leaderboardService: leaderboardService,
	}
}

// GetLeaderboard returns the user leaderboard
// GET /api/v1/leaderboard
// Query params: period (today/week/month/all), limit
func (h *LeaderboardHandler) GetLeaderboard(c *gin.Context) {
	period := strings.ToLower(c.DefaultQuery("period", "today"))
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	// Calculate time range based on period
	userTZ := c.Query("timezone")
	now := timezone.NowInUserLocation(userTZ)
	var startTime, endTime time.Time

	switch period {
	case "today":
		startTime = timezone.StartOfDayInUserLocation(now, userTZ)
		endTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)
	case "week":
		startTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, -7), userTZ)
		endTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)
	case "month":
		startTime = timezone.StartOfDayInUserLocation(now.AddDate(0, -1, 0), userTZ)
		endTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)
	case "all":
		startTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		endTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)
	default:
		startTime = timezone.StartOfDayInUserLocation(now, userTZ)
		endTime = timezone.StartOfDayInUserLocation(now.AddDate(0, 0, 1), userTZ)
	}

	// Get current user ID from context (set by auth middleware)
	currentUserID, _ := c.Get("user_id")

	leaderboard, err := h.leaderboardService.GetLeaderboard(
		c.Request.Context(),
		startTime,
		endTime,
		limit,
		currentUserID,
	)
	if err != nil {
		response.Error(c, 500, "Failed to get leaderboard")
		return
	}

	response.Success(c, gin.H{
		"leaderboard": leaderboard,
		"period":      period,
		"start_date":  startTime.Format("2006-01-02"),
		"end_date":    endTime.Add(-24 * time.Hour).Format("2006-01-02"),
	})
}
