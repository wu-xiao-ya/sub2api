package service

import (
	"context"
	"errors"
	"fmt"
)

func (s *adminServiceImpl) ListAccountPoolGroups(ctx context.Context) ([]AccountPoolGroup, error) {
	if s == nil || s.accountPoolGroupRepo == nil {
		return []AccountPoolGroup{}, nil
	}
	return s.accountPoolGroupRepo.List(ctx)
}

func (s *adminServiceImpl) CreateAccountPoolGroup(ctx context.Context, input *CreateAccountPoolGroupInput) (*AccountPoolGroup, error) {
	if input == nil {
		return nil, ErrAccountPoolGroupNameRequired
	}
	group, err := normalizeAccountPoolGroup(AccountPoolGroup{
		Name:        input.Name,
		UpstreamKey: input.UpstreamKey,
		Description: input.Description,
		SortOrder:   input.SortOrder,
		Status:      input.Status,
	})
	if err != nil {
		return nil, err
	}
	if s == nil || s.accountPoolGroupRepo == nil {
		return nil, errors.New("account pool group repository is not configured")
	}
	if err := s.accountPoolGroupRepo.Create(ctx, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func (s *adminServiceImpl) UpdateAccountPoolGroup(ctx context.Context, id int64, input *UpdateAccountPoolGroupInput) (*AccountPoolGroup, error) {
	if id <= 0 {
		return nil, ErrAccountPoolGroupNotFound
	}
	if input == nil {
		return nil, ErrAccountPoolGroupNameRequired
	}
	if s == nil || s.accountPoolGroupRepo == nil {
		return nil, errors.New("account pool group repository is not configured")
	}
	group, err := s.accountPoolGroupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		group.Name = input.Name
	}
	if input.UpstreamKey != nil {
		group.UpstreamKey = *input.UpstreamKey
	}
	if input.Description != nil {
		group.Description = *input.Description
	}
	if input.SortOrder != nil {
		group.SortOrder = *input.SortOrder
	}
	if input.Status != "" {
		group.Status = input.Status
	}
	normalized, err := normalizeAccountPoolGroup(*group)
	if err != nil {
		return nil, err
	}
	normalized.ID = group.ID
	normalized.CreatedAt = group.CreatedAt
	if err := s.accountPoolGroupRepo.Update(ctx, &normalized); err != nil {
		return nil, err
	}
	updated, err := s.accountPoolGroupRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *adminServiceImpl) DeleteAccountPoolGroup(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrAccountPoolGroupNotFound
	}
	if s == nil || s.accountPoolGroupRepo == nil {
		return errors.New("account pool group repository is not configured")
	}
	return s.accountPoolGroupRepo.Delete(ctx, id)
}

func (s *adminServiceImpl) validateAccountPoolGroupID(ctx context.Context, poolGroupID *int64) error {
	if poolGroupID == nil || *poolGroupID <= 0 {
		return nil
	}
	if s == nil || s.accountPoolGroupRepo == nil {
		return errors.New("account pool group repository is not configured")
	}
	if _, err := s.accountPoolGroupRepo.GetByID(ctx, *poolGroupID); err != nil {
		return fmt.Errorf("validate account pool group: %w", err)
	}
	return nil
}
