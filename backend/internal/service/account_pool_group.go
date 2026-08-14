package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrAccountPoolGroupNotFound     = infraerrors.NotFound("ACCOUNT_POOL_GROUP_NOT_FOUND", "account pool group not found")
	ErrAccountPoolGroupExists       = infraerrors.Conflict("ACCOUNT_POOL_GROUP_EXISTS", "account pool group already exists")
	ErrAccountPoolGroupNameRequired = infraerrors.New(http.StatusBadRequest, "ACCOUNT_POOL_GROUP_NAME_REQUIRED", "account pool group name is required")
)

const AccountListPoolGroupUngrouped int64 = -1

const (
	AccountPoolGroupStatusActive   = "active"
	AccountPoolGroupStatusInactive = "inactive"
)

type AccountPoolGroup struct {
	ID          int64
	Name        string
	UpstreamKey string
	Description string
	SortOrder   int
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateAccountPoolGroupInput struct {
	Name        string
	UpstreamKey string
	Description string
	SortOrder   int
	Status      string
}

type UpdateAccountPoolGroupInput struct {
	Name        string
	UpstreamKey *string
	Description *string
	SortOrder   *int
	Status      string
}

type AccountPoolGroupRepository interface {
	List(ctx context.Context) ([]AccountPoolGroup, error)
	GetByID(ctx context.Context, id int64) (*AccountPoolGroup, error)
	Create(ctx context.Context, group *AccountPoolGroup) error
	Update(ctx context.Context, group *AccountPoolGroup) error
	Delete(ctx context.Context, id int64) error
}

// AccountPoolGroupFilteredLister is implemented by account repositories that
// can filter account list pages by the admin-only pool group column. Keeping it
// separate avoids forcing gateway/test repositories to grow a management-only
// method.
type AccountPoolGroupFilteredLister interface {
	ListWithPoolGroupFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string, poolGroupID int64) ([]Account, *pagination.PaginationResult, error)
	ListAllWithPoolGroupFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string, poolGroupID int64) ([]Account, error)
}

func normalizeAccountPoolGroup(input AccountPoolGroup) (AccountPoolGroup, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.UpstreamKey = strings.TrimSpace(input.UpstreamKey)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" {
		return input, ErrAccountPoolGroupNameRequired
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.Status == "" {
		input.Status = AccountPoolGroupStatusActive
	}
	if input.Status != AccountPoolGroupStatusActive && input.Status != AccountPoolGroupStatusInactive {
		return input, infraerrors.BadRequest("ACCOUNT_POOL_GROUP_STATUS_INVALID", "account pool group status must be active or inactive")
	}
	return input, nil
}

func normalizeAccountPoolGroupID(poolGroupID *int64) *int64 {
	if poolGroupID == nil || *poolGroupID <= 0 {
		return nil
	}
	out := *poolGroupID
	return &out
}
