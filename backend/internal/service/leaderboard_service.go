package service

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagesource"
)

// LeaderboardService handles leaderboard business logic
type LeaderboardService struct {
	db *sql.DB
}

// NewLeaderboardService creates a new leaderboard service
func NewLeaderboardService(db *sql.DB) *LeaderboardService {
	return &LeaderboardService{
		db: db,
	}
}

// LeaderboardEntry represents a single entry in the leaderboard
type LeaderboardEntry struct {
	Rank          int     `json:"rank"`
	UserID        int64   `json:"user_id"`
	DisplayName   string  `json:"display_name"`
	Avatar        string  `json:"avatar,omitempty"`
	TotalTokens   int64   `json:"total_tokens"`
	Requests      int64   `json:"requests"`
	Cost          float64 `json:"cost"`
	IsCurrentUser bool    `json:"is_current_user"`
}

// GetLeaderboard returns the leaderboard for the specified time range
func (s *LeaderboardService) GetLeaderboard(
	ctx context.Context,
	startTime, endTime time.Time,
	limit int,
	currentUserID int64,
) ([]LeaderboardEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("leaderboard database is unavailable")
	}
	if limit < 1 || limit > 100 || !endTime.After(startTime) {
		return nil, fmt.Errorf("invalid leaderboard range or limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := `
		SELECT
			u.id as user_id,
			u.email,
			u.username,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) as total_tokens,
			COUNT(*) as requests,
			COALESCE(SUM(ul.actual_cost), 0) as cost
		FROM usage_logs ul
		JOIN users u ON u.id = ul.user_id
		WHERE u.status = 'active'
			AND u.deleted_at IS NULL
			AND COALESCE(u.is_monitoring_user, false) = false
			AND (ul.usage_source IS NULL OR ul.usage_source <> $4)
			AND ul.created_at >= $1
			AND ul.created_at < $2
		GROUP BY u.id, u.email, u.username
		HAVING COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) > 0
		ORDER BY total_tokens DESC, u.id ASC
		LIMIT $3
	`

	rows, err := s.db.QueryContext(ctx, query, startTime, endTime, limit, usagesource.ChannelMonitor)
	if err != nil {
		return nil, fmt.Errorf("failed to query leaderboard: %w", err)
	}
	defer rows.Close()

	entries := make([]LeaderboardEntry, 0, limit)
	rank := 1

	for rows.Next() {
		var (
			userID      int64
			email       string
			username    *string
			totalTokens int64
			requests    int64
			cost        float64
		)

		if err := rows.Scan(&userID, &email, &username, &totalTokens, &requests, &cost); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Mask email for privacy: t***v@gmail.com
		displayName := maskLeaderboardEmail(email)
		if username != nil && strings.TrimSpace(*username) != "" {
			displayName = strings.TrimSpace(*username)
		}

		// Generate consistent avatar based on user ID
		avatar := generateAvatar(userID)

		isCurrentUser := currentUserID > 0 && userID == currentUserID

		entries = append(entries, LeaderboardEntry{
			Rank:          rank,
			UserID:        userID,
			DisplayName:   displayName,
			Avatar:        avatar,
			TotalTokens:   totalTokens,
			Requests:      requests,
			Cost:          cost,
			IsCurrentUser: isCurrentUser,
		})
		rank++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read leaderboard: %w", err)
	}

	return entries, nil
}

// maskLeaderboardEmail masks an email address for privacy
// Example: test@example.com -> t***t@example.com
func maskLeaderboardEmail(email string) string {
	if len(email) == 0 {
		return "***"
	}

	// Find @ position
	atPos := -1
	for i, c := range email {
		if c == '@' {
			atPos = i
			break
		}
	}

	if atPos <= 0 {
		return "***"
	}

	// Keep first and last character of local part
	local := []rune(email[:atPos])
	domain := email[atPos:]

	if len(local) <= 2 {
		return string(local[0]) + "***" + domain
	}

	return string(local[0]) + "***" + string(local[len(local)-1]) + domain
}

// generateAvatar generates a consistent avatar identifier based on user ID
func generateAvatar(userID int64) string {
	// Generate MD5 hash of user ID for consistent color/icon
	hash := md5.Sum([]byte(fmt.Sprintf("%d", userID)))
	return fmt.Sprintf("#%02x%02x%02x", hash[0], hash[1], hash[2])
}
