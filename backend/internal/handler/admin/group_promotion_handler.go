package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type GroupPromotionHandler struct {
	service *service.GroupPromotionService
}

func NewGroupPromotionHandler(service *service.GroupPromotionService) *GroupPromotionHandler {
	return &GroupPromotionHandler{service: service}
}

type CreateGroupPromotionRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
	GroupID     int64   `json:"group_id" binding:"required,gt=0"`
	Mode        string  `json:"mode" binding:"required,oneof=discount_factor fixed_multiplier"`
	Value       float64 `json:"value"`
	StartsAt    int64   `json:"starts_at" binding:"required,gt=0"`
	EndsAt      int64   `json:"ends_at" binding:"required,gt=0"`
	Enabled     *bool   `json:"enabled"`
}

type UpdateGroupPromotionRequest struct {
	Name        *string  `json:"name"`
	Description **string `json:"description"`
	GroupID     *int64   `json:"group_id"`
	Mode        *string  `json:"mode" binding:"omitempty,oneof=discount_factor fixed_multiplier"`
	Value       *float64 `json:"value"`
	StartsAt    *int64   `json:"starts_at"`
	EndsAt      *int64   `json:"ends_at"`
	Enabled     *bool    `json:"enabled"`
}

func (h *GroupPromotionHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filters := service.GroupPromotionListFilters{
		Search: strings.TrimSpace(c.Query("search")),
	}
	if rawGroupID := strings.TrimSpace(c.Query("group_id")); rawGroupID != "" {
		groupID, err := strconv.ParseInt(rawGroupID, 10, 64)
		if err != nil || groupID <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filters.GroupID = &groupID
	}
	if rawEnabled := strings.TrimSpace(c.Query("enabled")); rawEnabled != "" {
		enabled, err := strconv.ParseBool(rawEnabled)
		if err != nil {
			response.BadRequest(c, "Invalid enabled")
			return
		}
		filters.Enabled = &enabled
	}
	items, result, err := h.service.List(c.Request.Context(), pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.GroupPromotion, 0, len(items))
	for i := range items {
		out = append(out, *dto.GroupPromotionFromService(&items[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

func (h *GroupPromotionHandler) GetByID(c *gin.Context) {
	id, ok := parseGroupPromotionID(c)
	if !ok {
		return
	}
	item, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.GroupPromotionFromService(item))
}

func (h *GroupPromotionHandler) Create(c *gin.Context) {
	var req CreateGroupPromotionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	actorID, ok := groupPromotionActorID(c)
	if !ok {
		return
	}
	item, err := h.service.Create(c.Request.Context(), &service.CreateGroupPromotionInput{
		Name:        req.Name,
		Description: req.Description,
		GroupID:     req.GroupID,
		Mode:        req.Mode,
		Value:       req.Value,
		StartsAt:    time.Unix(req.StartsAt, 0),
		EndsAt:      time.Unix(req.EndsAt, 0),
		Enabled:     req.Enabled,
		ActorID:     actorID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.GroupPromotionFromService(item))
}

func (h *GroupPromotionHandler) Update(c *gin.Context) {
	id, ok := parseGroupPromotionID(c)
	if !ok {
		return
	}
	var req UpdateGroupPromotionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	actorID, ok := groupPromotionActorID(c)
	if !ok {
		return
	}
	input := &service.UpdateGroupPromotionInput{
		Name:        req.Name,
		Description: req.Description,
		GroupID:     req.GroupID,
		Mode:        req.Mode,
		Value:       req.Value,
		Enabled:     req.Enabled,
		ActorID:     actorID,
	}
	if req.StartsAt != nil {
		if *req.StartsAt <= 0 {
			response.BadRequest(c, "starts_at must be a Unix timestamp")
			return
		}
		value := time.Unix(*req.StartsAt, 0)
		input.StartsAt = &value
	}
	if req.EndsAt != nil {
		if *req.EndsAt <= 0 {
			response.BadRequest(c, "ends_at must be a Unix timestamp")
			return
		}
		value := time.Unix(*req.EndsAt, 0)
		input.EndsAt = &value
	}
	item, err := h.service.Update(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.GroupPromotionFromService(item))
}

func (h *GroupPromotionHandler) Delete(c *gin.Context) {
	id, ok := parseGroupPromotionID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Group promotion deleted successfully"})
}

func parseGroupPromotionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid group promotion ID")
		return 0, false
	}
	return id, true
}

func groupPromotionActorID(c *gin.Context) (*int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return nil, false
	}
	return &subject.UserID, true
}
