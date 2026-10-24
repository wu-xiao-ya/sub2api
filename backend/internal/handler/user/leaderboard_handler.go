package user

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
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
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	period := strings.ToLower(c.DefaultQuery("period", "today"))
	switch period {
	case "today", "week", "month", "all":
	default:
		response.BadRequest(c, "Invalid period")
		return
	}
	limit := 20
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			response.BadRequest(c, "Invalid limit, use 1-100")
			return
		}
		limit = n
	}

	// Calculate time range based on period
	userTZ := c.Query("timezone")
	now := timezone.NowInUserLocation(userTZ)
	startTime, endTime := leaderboardRange(period, now)

	leaderboard, err := h.leaderboardService.GetLeaderboard(
		c.Request.Context(),
		startTime,
		endTime,
		limit,
		subject.UserID,
	)
	if err != nil {
		response.Error(c, 500, "Failed to get leaderboard")
		return
	}

	response.Success(c, gin.H{
		"leaderboard": leaderboard,
		"period":      period,
		"start_date":  startTime.Format("2006-01-02"),
		"end_date":    endTime.AddDate(0, 0, -1).Format("2006-01-02"),
	})
}

func leaderboardRange(period string, now time.Time) (time.Time, time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := today.AddDate(0, 0, 1)
	switch period {
	case "week":
		daysSinceMonday := (int(today.Weekday()) + 6) % 7
		return today.AddDate(0, 0, -daysSinceMonday), end
	case "month":
		return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, now.Location()), end
	case "all":
		return time.Unix(0, 0).UTC(), end
	default:
		return today, end
	}
}
