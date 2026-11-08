package admin

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) WorkBuddyModels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	ctx := c.Request.Context()
	account, err := h.adminService.GetAccount(ctx, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Type != service.AccountTypePlugin || account.Platform != service.PlatformDeepseek {
		response.BadRequest(c, "WorkBuddy account required")
		return
	}
	refresh := c.Request.Method == http.MethodPost
	var selection []string
	if !refresh {
		var body struct {
			Models *[]string `json:"models"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		if c.ShouldBindJSON(&body) != nil || body.Models == nil || len(*body.Models) > 256 {
			response.BadRequest(c, "Invalid model selection")
			return
		}
		selection = append([]string{}, *body.Models...)
		slices.Sort(selection)
		selection = slices.Compact(selection)
	}
	models, err := h.accountTestService.WorkBuddyCatalog(ctx, account, refresh)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "WorkBuddy model catalog unavailable")
		return
	}
	if !refresh {
		for _, model := range selection {
			if !slices.Contains(models, model) {
				response.BadRequest(c, "A selected model is outside the WorkBuddy catalog")
				return
			}
		}
		if err := h.adminService.UpdateAccountExtra(ctx, id, map[string]any{service.WorkBuddyModelsExtraKey: selection}); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	} else if configuredModels, configured := account.WorkBuddyModelSelection(); configured {
		selection = append([]string{}, configuredModels...)
	} else {
		selection = []string{}
		for _, model := range models {
			if account.IsModelSupported(model) {
				selection = append(selection, model)
			}
		}
	}
	response.Success(c, gin.H{"models": models, "enabled_models": selection})
}
