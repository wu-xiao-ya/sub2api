package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newPlanGroupTestService(t *testing.T) (*PaymentConfigService, *dbent.Client) {
	t.Helper()
	dbName := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	db, err := sql.Open("sqlite", dbName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	return &PaymentConfigService{entClient: client, sqlDB: db}, client
}

func createPlanTestGroup(t *testing.T, client *dbent.Client, name, platform string, allowImage bool) *dbent.Group {
	t.Helper()
	group, err := client.Group.Create().
		SetName(name).
		SetPlatform(platform).
		SetStatus(StatusActive).
		SetSubscriptionType(SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowImageGeneration(allowImage).
		Save(context.Background())
	require.NoError(t, err)
	return group
}

func TestValidatePlanGroupsAllowsMixedPlatforms(t *testing.T) {
	svc, client := newPlanGroupTestService(t)
	ctx := context.Background()
	openai := createPlanTestGroup(t, client, "gpt", PlatformOpenAI, false)
	claude := createPlanTestGroup(t, client, "claude", PlatformAnthropic, false)
	gemini := createPlanTestGroup(t, client, "gemini-image", PlatformGemini, true)

	ids, err := svc.validatePlanGroups(ctx, openai.ID, []int64{int64(claude.ID), int64(gemini.ID)})
	require.NoError(t, err)
	require.Equal(t, []int64{int64(openai.ID), int64(claude.ID), int64(gemini.ID)}, ids)
}

func TestValidatePlanGroupsRejectsMissingGroup(t *testing.T) {
	svc, client := newPlanGroupTestService(t)
	openai := createPlanTestGroup(t, client, "gpt", PlatformOpenAI, false)
	_, err := svc.validatePlanGroups(context.Background(), openai.ID, []int64{int64(openai.ID), 99999})
	require.Error(t, err)
	require.Equal(t, "PLAN_GROUP_NOT_FOUND", errors.Reason(err))
}

func TestCreatePlanPersistsMixedPlatformGroups(t *testing.T) {
	svc, client := newPlanGroupTestService(t)
	ctx := context.Background()
	openai := createPlanTestGroup(t, client, "gpt", PlatformOpenAI, false)
	claude := createPlanTestGroup(t, client, "claude", PlatformAnthropic, false)
	kimi := createPlanTestGroup(t, client, "kimi", PlatformKimi, false)

	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{
		Name:         "mixed",
		Description:  "gpt+claude+kimi",
		GroupID:      int64(openai.ID),
		GroupIDs:     []int64{int64(openai.ID), int64(claude.ID), int64(kimi.ID)},
		Price:        30,
		ValidityDays: 30,
		ValidityUnit: "days",
		ForSale:      true,
	})
	require.NoError(t, err)

	ids, err := svc.ListPlanGroupIDs(ctx, plan)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{int64(openai.ID), int64(claude.ID), int64(kimi.ID)}, ids)

	info := svc.GetGroupInfoMap(ctx, []*dbent.SubscriptionPlan{plan})
	require.Equal(t, PlatformOpenAI, info[int64(openai.ID)].Platform)
	require.Equal(t, PlatformAnthropic, info[int64(claude.ID)].Platform)
	require.Equal(t, PlatformKimi, info[int64(kimi.ID)].Platform)

	groups := BuildPublicPlanGroups(ids, info)
	require.Len(t, groups, 3)
	platforms := make([]string, 0, len(groups))
	for _, group := range groups {
		platforms = append(platforms, group.Platform)
	}
	require.ElementsMatch(t, []string{PlatformOpenAI, PlatformAnthropic, PlatformKimi}, platforms)
}

func TestValidateSubOrderAllowsMixedPlatformPlan(t *testing.T) {
	svc, client := newPlanGroupTestService(t)
	ctx := context.Background()
	openai := createPlanTestGroup(t, client, "gpt", PlatformOpenAI, false)
	claude := createPlanTestGroup(t, client, "claude", PlatformAnthropic, false)
	plan, err := svc.CreatePlan(ctx, CreatePlanRequest{
		Name:         "mixed",
		Description:  "gpt+claude",
		GroupID:      int64(openai.ID),
		GroupIDs:     []int64{int64(openai.ID), int64(claude.ID)},
		Price:        20,
		ValidityDays: 7,
		ValidityUnit: "days",
		ForSale:      true,
	})
	require.NoError(t, err)

	pay := &PaymentService{
		configService: svc,
		groupRepo: &planGroupRepoStub{groups: map[int64]*Group{
			int64(openai.ID): {ID: int64(openai.ID), Status: StatusActive, Platform: PlatformOpenAI, Hydrated: true},
			int64(claude.ID): {ID: int64(claude.ID), Status: StatusActive, Platform: PlatformAnthropic, Hydrated: true},
		}},
	}
	got, err := pay.validateSubOrder(ctx, CreateOrderRequest{PlanID: int64(plan.ID)})
	require.NoError(t, err)
	require.Equal(t, plan.ID, got.ID)
}

func TestBuildPublicPlanGroupsUsesFallbackName(t *testing.T) {
	groups := BuildPublicPlanGroups([]int64{8}, map[int64]PlanGroupInfo{
		8: {Platform: PlatformGLM},
	})
	require.Equal(t, []PublicPlanGroup{{ID: 8, Name: "#8", Platform: PlatformGLM}}, groups)
}

type planGroupRepoStub struct {
	groupRepoNoop
	groups map[int64]*Group
}

func (s *planGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, fmt.Errorf("group %d not found", id)
	}
	return group, nil
}

var _ GroupRepository = (*planGroupRepoStub)(nil)
