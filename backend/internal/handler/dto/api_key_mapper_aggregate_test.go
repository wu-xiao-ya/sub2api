package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromService_MapsAggregateMembersWithoutSecrets(t *testing.T) {
	gid := int64(11)
	src := &service.APIKey{
		ID:      99,
		UserID:  7,
		Key:     "sk-aggregate-secret",
		Name:    "mix",
		Status:  service.StatusActive,
		KeyKind: service.APIKeyKindAggregate,
		Members: []*service.APIKey{
			{
				ID:      1,
				UserID:  7,
				Key:     "sk-member-secret",
				Name:    "gpt",
				Status:  service.StatusActive,
				KeyKind: service.APIKeyKindGroup,
				GroupID: &gid,
				Group:   &service.Group{ID: gid, Name: "gpt", Platform: service.PlatformOpenAI, Status: service.StatusActive},
			},
		},
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.Equal(t, service.APIKeyKindAggregate, out.KeyKind)
	require.Equal(t, "sk-aggregate-secret", out.Key)
	require.Len(t, out.Members, 1)
	require.Equal(t, int64(1), out.Members[0].ID)
	require.Equal(t, "gpt", out.Members[0].Name)
	require.NotNil(t, out.Members[0].GroupID)
	require.Equal(t, gid, *out.Members[0].GroupID)
	require.NotNil(t, out.Members[0].Group)
	require.Equal(t, "gpt", out.Members[0].Group.Name)

	raw, err := json.Marshal(out.Members[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sk-member-secret")
	require.NotContains(t, string(raw), `"key"`)
}
