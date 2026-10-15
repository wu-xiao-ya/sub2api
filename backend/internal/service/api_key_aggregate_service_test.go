//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type aggregateAPIKeyStoreStub struct {
	apiKeyRepoStub
	keysByID           map[int64]*APIKey
	kinds              map[int64]string
	members            map[int64][]int64
	deletedMemberships []int64
	nextID             int64
	createErr          error
	setKindErr         error
	replaceErr         error
}

func (s *aggregateAPIKeyStoreStub) Create(_ context.Context, key *APIKey) error {
	if s.createErr != nil {
		return s.createErr
	}
	if key.ID == 0 {
		if s.nextID == 0 {
			s.nextID = 100
		}
		s.nextID++
		key.ID = s.nextID
	}
	clone := *key
	if len(key.Members) > 0 {
		clone.Members = append([]*APIKey(nil), key.Members...)
	}
	if s.keysByID == nil {
		s.keysByID = map[int64]*APIKey{}
	}
	stored := clone
	s.keysByID[key.ID] = &stored
	s.apiKey = &stored
	return nil
}

func (s *aggregateAPIKeyStoreStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	if key, ok := s.keysByID[id]; ok {
		clone := *key
		if len(key.Members) > 0 {
			clone.Members = append([]*APIKey(nil), key.Members...)
		}
		return &clone, nil
	}
	return s.apiKeyRepoStub.GetByID(context.Background(), id)
}

func (s *aggregateAPIKeyStoreStub) ExistsByKey(_ context.Context, key string) (bool, error) {
	for _, item := range s.keysByID {
		if item != nil && item.Key == key {
			return true, nil
		}
	}
	return false, nil
}

func (s *aggregateAPIKeyStoreStub) SetKeyKind(_ context.Context, id int64, kind string) error {
	if s.setKindErr != nil {
		return s.setKindErr
	}
	if s.kinds == nil {
		s.kinds = map[int64]string{}
	}
	s.kinds[id] = NormalizeAPIKeyKind(kind)
	if key, ok := s.keysByID[id]; ok {
		key.KeyKind = NormalizeAPIKeyKind(kind)
	}
	return nil
}

func (s *aggregateAPIKeyStoreStub) ReplaceAggregateMembers(_ context.Context, aggregateID int64, memberIDs []int64) error {
	if s.replaceErr != nil {
		return s.replaceErr
	}
	if s.members == nil {
		s.members = map[int64][]int64{}
	}
	s.members[aggregateID] = append([]int64(nil), memberIDs...)
	return nil
}

func (s *aggregateAPIKeyStoreStub) DeleteAggregateMembershipsForKey(_ context.Context, keyID int64) error {
	s.deletedMemberships = append(s.deletedMemberships, keyID)
	if s.members == nil {
		return nil
	}
	delete(s.members, keyID)
	for aggregateID, ids := range s.members {
		filtered := make([]int64, 0, len(ids))
		for _, id := range ids {
			if id != keyID {
				filtered = append(filtered, id)
			}
		}
		s.members[aggregateID] = filtered
	}
	return nil
}

func (s *aggregateAPIKeyStoreStub) ListAggregateKeysByMemberID(_ context.Context, memberID int64) ([]string, error) {
	var keys []string
	for aggregateID, ids := range s.members {
		found := false
		for _, id := range ids {
			if id == memberID {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if key, ok := s.keysByID[aggregateID]; ok && key.Key != "" {
			keys = append(keys, key.Key)
		}
	}
	return keys, nil
}

func seedGroupKey(id, userID, groupID int64, platform, name string) *APIKey {
	gid := groupID
	return &APIKey{
		ID:      id,
		UserID:  userID,
		Key:     fmt.Sprintf("sk-member-%d", id),
		Name:    name,
		Status:  StatusActive,
		KeyKind: APIKeyKindGroup,
		GroupID: &gid,
		Group:   &Group{ID: gid, Name: name, Status: StatusActive, Platform: platform},
	}
}

func newAggregateKeyService(t *testing.T, repo *aggregateAPIKeyStoreStub, userID int64) *APIKeyService {
	t.Helper()
	return NewAPIKeyService(
		repo,
		&userRepoStub{user: &User{ID: userID, Status: StatusActive, Role: RoleUser}},
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)
}

func TestAPIKeyService_Create_AggregatePersistsMembers(t *testing.T) {
	gpt := seedGroupKey(1, 7, 11, PlatformOpenAI, "gpt")
	claude := seedGroupKey(2, 7, 12, PlatformAnthropic, "claude")
	repo := &aggregateAPIKeyStoreStub{
		keysByID: map[int64]*APIKey{1: gpt, 2: claude},
	}
	svc := newAggregateKeyService(t, repo, 7)

	created, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "mix",
		KeyKind:      APIKeyKindAggregate,
		MemberKeyIDs: []int64{1, 2},
	})

	require.NoError(t, err)
	require.Equal(t, APIKeyKindAggregate, created.KeyKind)
	require.Nil(t, created.GroupID)
	require.Equal(t, []int64{1, 2}, created.MemberIDs())
	require.Equal(t, APIKeyKindAggregate, repo.kinds[created.ID])
	require.Equal(t, []int64{1, 2}, repo.members[created.ID])
}

func TestAPIKeyService_Create_AggregateRejectsGroupBindNestedAndForeign(t *testing.T) {
	gid := int64(11)
	repo := &aggregateAPIKeyStoreStub{
		keysByID: map[int64]*APIKey{
			1: seedGroupKey(1, 7, 11, PlatformOpenAI, "gpt"),
			2: {ID: 2, UserID: 7, Status: StatusActive, KeyKind: APIKeyKindAggregate, GroupID: &gid},
			3: seedGroupKey(3, 8, 12, PlatformAnthropic, "other"),
			4: seedGroupKey(4, 7, 11, PlatformOpenAI, "gpt-dup"),
		},
	}
	svc := newAggregateKeyService(t, repo, 7)

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "bad-group",
		KeyKind:      APIKeyKindAggregate,
		GroupID:      &gid,
		MemberKeyIDs: []int64{1},
	})
	require.ErrorIs(t, err, ErrAggregateCannotBindGroup)

	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "nested",
		KeyKind:      APIKeyKindAggregate,
		MemberKeyIDs: []int64{1, 2},
	})
	require.ErrorIs(t, err, ErrAggregateNested)

	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "foreign",
		KeyKind:      APIKeyKindAggregate,
		MemberKeyIDs: []int64{3},
	})
	require.ErrorIs(t, err, ErrAggregateMemberNotOwned)

	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "dup-group",
		KeyKind:      APIKeyKindAggregate,
		MemberKeyIDs: []int64{1, 4},
	})
	require.ErrorIs(t, err, ErrAggregateDuplicateGroup)

	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "group-with-members",
		KeyKind:      APIKeyKindGroup,
		GroupID:      &gid,
		MemberKeyIDs: []int64{1},
	})
	require.ErrorIs(t, err, ErrGroupKeyCannotHaveMembers)
	require.Empty(t, repo.members)
}

func TestAPIKeyService_Create_AggregatePersistFailureRollsBack(t *testing.T) {
	repo := &aggregateAPIKeyStoreStub{
		keysByID:   map[int64]*APIKey{1: seedGroupKey(1, 7, 11, PlatformOpenAI, "gpt")},
		replaceErr: errors.New("write members failed"),
	}
	svc := newAggregateKeyService(t, repo, 7)

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:         "mix",
		KeyKind:      APIKeyKindAggregate,
		MemberKeyIDs: []int64{1},
	})
	require.Error(t, err)
	require.NotEmpty(t, repo.deletedIDs)
	require.Equal(t, repo.deletedIDs, repo.deletedMemberships)
}

func TestAPIKeyService_Update_AggregateReplacesMembers(t *testing.T) {
	gpt := seedGroupKey(1, 7, 11, PlatformOpenAI, "gpt")
	claude := seedGroupKey(2, 7, 12, PlatformAnthropic, "claude")
	glm := seedGroupKey(3, 7, 13, PlatformGLM, "glm")
	aggregate := &APIKey{
		ID:      50,
		UserID:  7,
		Key:     "sk-agg-50",
		Name:    "mix",
		Status:  StatusActive,
		KeyKind: APIKeyKindAggregate,
		Members: []*APIKey{gpt, claude},
	}
	repo := &aggregateAPIKeyStoreStub{
		apiKeyRepoStub: apiKeyRepoStub{apiKey: aggregate},
		keysByID: map[int64]*APIKey{
			1:  gpt,
			2:  claude,
			3:  glm,
			50: aggregate,
		},
		members: map[int64][]int64{50: {1, 2}},
		kinds:   map[int64]string{50: APIKeyKindAggregate},
	}
	svc := newAggregateKeyService(t, repo, 7)
	memberIDs := []int64{3, 1}

	updated, err := svc.Update(context.Background(), 50, 7, UpdateAPIKeyRequest{MemberKeyIDs: &memberIDs})
	require.NoError(t, err)
	require.Equal(t, []int64{3, 1}, updated.MemberIDs())
	require.Equal(t, []int64{3, 1}, repo.members[50])

	gid := int64(11)
	_, err = svc.Update(context.Background(), 50, 7, UpdateAPIKeyRequest{GroupID: &gid})
	require.ErrorIs(t, err, ErrAggregateCannotBindGroup)
}

func TestAPIKeyService_Delete_DetachesMembersAndInvalidatesParents(t *testing.T) {
	member := seedGroupKey(1, 7, 11, PlatformOpenAI, "gpt")
	aggregate := &APIKey{
		ID:      50,
		UserID:  7,
		Key:     "sk-agg-50",
		Status:  StatusActive,
		KeyKind: APIKeyKindAggregate,
		Members: []*APIKey{member},
	}
	repo := &aggregateAPIKeyStoreStub{
		apiKeyRepoStub: apiKeyRepoStub{apiKey: member},
		keysByID: map[int64]*APIKey{
			1:  member,
			50: aggregate,
		},
		members: map[int64][]int64{50: {1}},
	}
	svc := newAggregateKeyService(t, repo, 7)

	require.NoError(t, svc.Delete(context.Background(), 1, 7))
	require.Contains(t, repo.deletedIDs, int64(1))
	require.Contains(t, repo.deletedMemberships, int64(1))
	require.Empty(t, repo.members[50])
}
