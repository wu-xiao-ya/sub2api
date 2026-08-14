package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	openAIAccountRuntimeLogComponent       = "service.openai_account_runtime"
	openAIAccountNoCandidatesLogThrottle   = 30 * time.Second
	openAIAccountNoCandidatesLogMaxEntries = 1024
)

func writeOpenAIAccountRuntimeEvent(
	ctx context.Context,
	message string,
	account *Account,
	model string,
	fields map[string]any,
) {
	if strings.TrimSpace(message) == "" {
		return
	}

	eventFields := make(map[string]any, len(fields)+10)
	for key, value := range fields {
		eventFields[key] = value
	}
	if account != nil {
		eventFields["account_id"] = account.ID
		eventFields["account_name"] = strings.TrimSpace(account.Name)
		eventFields["platform"] = strings.TrimSpace(account.Platform)
	}
	if model = strings.TrimSpace(model); model != "" {
		eventFields["model"] = model
	}
	if groupID := openAIAccountRuntimeGroupID(ctx, account); groupID > 0 {
		eventFields["group_id"] = groupID
	}
	if requestID := contextString(ctx, ctxkey.RequestID); requestID != "" {
		eventFields["request_id"] = requestID
	}
	if clientRequestID := contextString(ctx, ctxkey.ClientRequestID); clientRequestID != "" {
		eventFields["client_request_id"] = clientRequestID
	}

	logger.WriteSinkEvent("info", openAIAccountRuntimeLogComponent, message, eventFields)
}

func contextString(ctx context.Context, key ctxkey.Key) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(key).(string)
	return strings.TrimSpace(value)
}

func openAIAccountRuntimeGroupID(ctx context.Context, account *Account) int64 {
	if ctx != nil {
		switch group := ctx.Value(ctxkey.Group).(type) {
		case *Group:
			if group != nil && group.ID > 0 {
				return group.ID
			}
		case Group:
			if group.ID > 0 {
				return group.ID
			}
		}
	}
	if account == nil {
		return 0
	}
	if account.PoolGroupID != nil && *account.PoolGroupID > 0 {
		return *account.PoolGroupID
	}
	if len(account.GroupIDs) == 1 {
		return account.GroupIDs[0]
	}
	return 0
}

func (s *OpenAIGatewayService) logOpenAIAccountSelectionNoCandidates(
	ctx context.Context,
	groupID *int64,
	platform string,
	model string,
	excludedIDs map[int64]struct{},
	reasonCounts map[string]int,
) {
	if s == nil {
		return
	}

	fingerprint := openAIAccountNoCandidatesFingerprint(groupID, platform, model, reasonCounts)
	now := time.Now()

	s.openaiNoCandidateLogMu.Lock()
	if s.openaiNoCandidateLogAt == nil {
		s.openaiNoCandidateLogAt = make(map[string]time.Time)
	}
	if last, ok := s.openaiNoCandidateLogAt[fingerprint]; ok && now.Sub(last) < openAIAccountNoCandidatesLogThrottle {
		s.openaiNoCandidateLogMu.Unlock()
		return
	}
	s.openaiNoCandidateLogAt[fingerprint] = now
	if len(s.openaiNoCandidateLogAt) > openAIAccountNoCandidatesLogMaxEntries {
		cutoff := now.Add(-2 * openAIAccountNoCandidatesLogThrottle)
		for key, timestamp := range s.openaiNoCandidateLogAt {
			if timestamp.Before(cutoff) {
				delete(s.openaiNoCandidateLogAt, key)
			}
		}
	}
	s.openaiNoCandidateLogMu.Unlock()

	excludedAccountIDs := make([]int64, 0, len(excludedIDs))
	for accountID := range excludedIDs {
		excludedAccountIDs = append(excludedAccountIDs, accountID)
	}
	sort.Slice(excludedAccountIDs, func(i, j int) bool {
		return excludedAccountIDs[i] < excludedAccountIDs[j]
	})

	fields := map[string]any{
		"platform":             strings.TrimSpace(platform),
		"model":                strings.TrimSpace(model),
		"excluded_account_ids": excludedAccountIDs,
		"exclude_reasons":      reasonCounts,
	}
	if groupID != nil && *groupID > 0 {
		fields["group_id"] = *groupID
	}
	writeOpenAIAccountRuntimeEvent(ctx, "account_selection_no_candidates", nil, model, fields)
}

func openAIAccountNoCandidatesFingerprint(groupID *int64, platform string, model string, reasonCounts map[string]int) string {
	reasons := make([]string, 0, len(reasonCounts))
	for reason, count := range reasonCounts {
		reasons = append(reasons, fmt.Sprintf("%s=%d", strings.TrimSpace(reason), count))
	}
	sort.Strings(reasons)
	return fmt.Sprintf(
		"%d|%s|%s|%s",
		derefGroupID(groupID),
		strings.ToLower(strings.TrimSpace(platform)),
		strings.ToLower(strings.TrimSpace(model)),
		strings.Join(reasons, ","),
	)
}
