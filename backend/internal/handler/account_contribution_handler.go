package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountContributionHandler struct {
	service             *service.AccountContributionService
	accountUsageService *service.AccountUsageService
}

func NewAccountContributionHandler(service *service.AccountContributionService, accountUsageService *service.AccountUsageService) *AccountContributionHandler {
	return &AccountContributionHandler{service: service, accountUsageService: accountUsageService}
}

type contributionAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

type submitOpenAIContributionRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	State       string `json:"state" binding:"required"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
	ProxyURL    string `json:"proxy_url"`
	Name        string `json:"name"`
}

type submitOpenAIJSONContributionRequest struct {
	Data     contributionDataPayload   `json:"data"`
	Accounts []contributionDataAccount `json:"accounts"`
	ProxyID  *int64                    `json:"proxy_id"`
	ProxyURL string                    `json:"proxy_url"`
}

type contributionDataPayload struct {
	Type     string                    `json:"type,omitempty"`
	Version  int                       `json:"version,omitempty"`
	Proxies  []contributionDataProxy   `json:"proxies"`
	Accounts []contributionDataAccount `json:"accounts"`
}

type contributionDataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Status   string `json:"status"`
}

type contributionDataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	ProxyKey           *string        `json:"proxy_key"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	ExpiresAt          *int64         `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
}

type approveContributionRequest struct {
	GroupIDs    []int64 `json:"group_ids" binding:"required"`
	Concurrency *int    `json:"concurrency"`
	Priority    *int    `json:"priority"`
}

type updateContributionConfigRequest struct {
	Name                     *string                          `json:"name"`
	Notes                    *string                          `json:"notes"`
	Concurrency              *int                             `json:"concurrency"`
	LoadFactor               *int                             `json:"load_factor"`
	ExpiresAt                *int64                           `json:"expires_at"`
	AutoPauseOnExpired       *bool                            `json:"auto_pause_on_expired"`
	TempUnschedulableEnabled *bool                            `json:"temp_unschedulable_enabled"`
	TempUnschedulableRules   *[]service.TempUnschedulableRule `json:"temp_unschedulable_rules"`
	AutoPause5hThreshold     *float64                         `json:"auto_pause_5h_threshold"`
	AutoPause7dThreshold     *float64                         `json:"auto_pause_7d_threshold"`
	AutoPause5hDisabled      *bool                            `json:"auto_pause_5h_disabled"`
	AutoPause7dDisabled      *bool                            `json:"auto_pause_7d_disabled"`
}

type contributionBatchTodayStatsRequest struct {
	AccountIDs []int64 `json:"account_ids" binding:"required"`
}

func contributionAccountsFromRequest(req submitOpenAIJSONContributionRequest) []service.OpenAIJSONContributionAccount {
	accounts := req.Accounts
	if len(accounts) == 0 {
		accounts = req.Data.Accounts
	}
	inputAccounts := make([]service.OpenAIJSONContributionAccount, 0, len(accounts))
	for i := range accounts {
		inputAccounts = append(inputAccounts, service.OpenAIJSONContributionAccount{
			Name:               accounts[i].Name,
			Notes:              accounts[i].Notes,
			Platform:           accounts[i].Platform,
			Type:               accounts[i].Type,
			Credentials:        accounts[i].Credentials,
			Extra:              accounts[i].Extra,
			ProxyKey:           accounts[i].ProxyKey,
			Concurrency:        accounts[i].Concurrency,
			Priority:           accounts[i].Priority,
			ExpiresAt:          accounts[i].ExpiresAt,
			AutoPauseOnExpired: accounts[i].AutoPauseOnExpired,
		})
	}
	return inputAccounts
}

func contributionProxiesFromRequest(req submitOpenAIJSONContributionRequest) []service.OpenAIJSONContributionProxy {
	proxies := req.Data.Proxies
	inputProxies := make([]service.OpenAIJSONContributionProxy, 0, len(proxies))
	for i := range proxies {
		inputProxies = append(inputProxies, service.OpenAIJSONContributionProxy{
			ProxyKey: proxies[i].ProxyKey,
			Name:     proxies[i].Name,
			Protocol: proxies[i].Protocol,
			Host:     proxies[i].Host,
			Port:     proxies[i].Port,
			Username: proxies[i].Username,
			Password: proxies[i].Password,
			Status:   proxies[i].Status,
		})
	}
	return inputProxies
}

func (h *AccountContributionHandler) GenerateOpenAIAuthURL(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req contributionAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.service.GenerateOpenAIAuthURL(c.Request.Context(), subject.UserID, req.ProxyID, req.RedirectURI)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *AccountContributionHandler) SubmitOpenAI(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.service.SubmitOpenAI(c.Request.Context(), subject.UserID, service.SubmitOpenAIContributionInput{
		SessionID: req.SessionID, Code: req.Code, State: req.State, RedirectURI: req.RedirectURI, ProxyID: req.ProxyID, ProxyURL: req.ProxyURL, Name: req.Name,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) SubmitOpenAIJSON(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIJSONContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.SubmitOpenAIJSON(c.Request.Context(), subject.UserID, service.SubmitOpenAIJSONContributionInput{
		Accounts: contributionAccountsFromRequest(req),
		Proxies:  contributionProxiesFromRequest(req),
		ProxyID:  req.ProxyID,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountContributionHandler) PreviewOpenAIJSON(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req submitOpenAIJSONContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	preview, err := h.service.PreviewOpenAIJSON(c.Request.Context(), subject.UserID, service.SubmitOpenAIJSONContributionInput{
		Accounts: contributionAccountsFromRequest(req),
		Proxies:  contributionProxiesFromRequest(req),
		ProxyID:  req.ProxyID,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *AccountContributionHandler) ListMine(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListMine(c.Request.Context(), subject.UserID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.Account, 0, len(items))
	for i := range items {
		out = append(out, *dto.AccountFromService(&items[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) Revoke(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.service.Revoke(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) Republish(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.service.Republish(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) UpdateConfig(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req updateContributionConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.service.UpdateMineConfig(c.Request.Context(), subject.UserID, id, service.ContributionAccountConfigInput{
		Name:                     req.Name,
		Notes:                    req.Notes,
		Concurrency:              req.Concurrency,
		LoadFactor:               req.LoadFactor,
		ExpiresAt:                req.ExpiresAt,
		AutoPauseOnExpired:       req.AutoPauseOnExpired,
		TempUnschedulableEnabled: req.TempUnschedulableEnabled,
		TempUnschedulableRules:   req.TempUnschedulableRules,
		AutoPause5hThreshold:     req.AutoPause5hThreshold,
		AutoPause7dThreshold:     req.AutoPause7dThreshold,
		AutoPause5hDisabled:      req.AutoPause5hDisabled,
		AutoPause7dDisabled:      req.AutoPause7dDisabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) GetUsage(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if _, err := h.service.GetMine(c.Request.Context(), subject.UserID, id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	source := c.DefaultQuery("source", "active")
	force := c.Query("force") == "true"
	var usage *service.UsageInfo
	if source == "passive" {
		usage, err = h.accountUsageService.GetPassiveUsage(c.Request.Context(), id)
	} else {
		usage, err = h.accountUsageService.GetUsage(c.Request.Context(), id, force)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, usage)
}

func (h *AccountContributionHandler) GetTodayStats(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if _, err := h.service.GetMine(c.Request.Context(), subject.UserID, id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	stats, err := h.accountUsageService.GetTodayStats(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, stats)
}

func (h *AccountContributionHandler) GetBatchTodayStats(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req contributionBatchTodayStatsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	seen := make(map[int64]struct{}, len(req.AccountIDs))
	accountIDs := make([]int64, 0, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		if _, err := h.service.GetMine(c.Request.Context(), subject.UserID, id); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		seen[id] = struct{}{}
		accountIDs = append(accountIDs, id)
	}
	if len(accountIDs) == 0 {
		response.Success(c, gin.H{"stats": map[string]any{}})
		return
	}
	stats, err := h.accountUsageService.GetTodayStatsBatch(c.Request.Context(), accountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"stats": stats})
}

func (h *AccountContributionHandler) ListRewards(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, result, err := h.service.ListRewards(c.Request.Context(), subject.UserID, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) GetRewardSummary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	summary, err := h.service.GetRewardSummary(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, summary)
}

func (h *AccountContributionHandler) ListPending(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	status := c.DefaultQuery("status", service.ContributionStatusPending)
	items, result, err := h.service.ListByStatus(c.Request.Context(), status, pagination.PaginationParams{Page: page, PageSize: pageSize})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.Account, 0, len(items))
	for i := range items {
		out = append(out, *dto.AccountFromService(&items[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *AccountContributionHandler) Approve(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req approveContributionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	account, err := h.service.Approve(c.Request.Context(), id, service.ApproveContributionInput{GroupIDs: req.GroupIDs, Concurrency: req.Concurrency, Priority: req.Priority})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func (h *AccountContributionHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.service.Reject(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}
