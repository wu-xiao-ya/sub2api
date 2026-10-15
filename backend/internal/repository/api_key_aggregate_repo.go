package repository

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"entgo.io/ent/dialect"
)

func (r *apiKeyRepository) SetKeyKind(ctx context.Context, id int64, kind string) error {
	if r.sql == nil {
		return nil
	}
	kind = service.NormalizeAPIKeyKind(kind)
	query, args := setAPIKeyKindQuery(id, kind, r.client.Driver().Dialect())
	_, err := r.sql.ExecContext(ctx, query, args...)
	if isMissingAggregateSchema(err) {
		return nil
	}
	return err
}

func (r *apiKeyRepository) ReplaceAggregateMembers(ctx context.Context, aggregateID int64, memberIDs []int64) error {
	if r.sql == nil {
		return fmt.Errorf("aggregate member store is unavailable")
	}
	dialectName := r.client.Driver().Dialect()
	deleteQuery, deleteArgs := deleteAggregateMembersQuery(aggregateID, dialectName)
	if _, err := r.sql.ExecContext(ctx, deleteQuery, deleteArgs...); err != nil {
		return err
	}
	if len(memberIDs) == 0 {
		return nil
	}
	insertQuery, insertArgs := insertAggregateMembersQuery(aggregateID, memberIDs, dialectName)
	_, err := r.sql.ExecContext(ctx, insertQuery, insertArgs...)
	if isMissingAggregateSchema(err) {
		return fmt.Errorf("aggregate member store is unavailable")
	}
	return err
}

func (r *apiKeyRepository) DeleteAggregateMembershipsForKey(ctx context.Context, keyID int64) error {
	if r.sql == nil {
		return nil
	}
	query, args := deleteAggregateMembershipsForKeyQuery(keyID, r.client.Driver().Dialect())
	_, err := r.sql.ExecContext(ctx, query, args...)
	return err
}

func (r *apiKeyRepository) ListAggregateKeysByMemberID(ctx context.Context, memberID int64) ([]string, error) {
	if r.sql == nil || memberID <= 0 {
		return nil, nil
	}
	query, args := listAggregateKeysByMemberIDQuery(memberID, r.client.Driver().Dialect())
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		if isMissingAggregateSchema(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *apiKeyRepository) attachAggregateMetadata(ctx context.Context, keys []service.APIKey) error {
	if len(keys) == 0 || r.sql == nil {
		return nil
	}
	ids := make([]int64, 0, len(keys))
	indexByID := make(map[int64][]int, len(keys))
	for i := range keys {
		ids = append(ids, keys[i].ID)
		indexByID[keys[i].ID] = append(indexByID[keys[i].ID], i)
		if keys[i].KeyKind == "" {
			keys[i].KeyKind = service.APIKeyKindGroup
		}
	}
	kinds, err := r.loadKeyKinds(ctx, ids)
	if err != nil {
		if isMissingAggregateSchema(err) {
			return nil
		}
		return err
	}
	aggregateIDs := make([]int64, 0)
	for id, kind := range kinds {
		for _, idx := range indexByID[id] {
			keys[idx].KeyKind = kind
		}
		if kind == service.APIKeyKindAggregate {
			aggregateIDs = append(aggregateIDs, id)
		}
	}
	if len(aggregateIDs) == 0 {
		return nil
	}
	membersByAggregate, err := r.loadAggregateMembers(ctx, aggregateIDs)
	if err != nil {
		if isMissingAggregateSchema(err) {
			return nil
		}
		return err
	}
	for aggregateID, members := range membersByAggregate {
		for _, idx := range indexByID[aggregateID] {
			keys[idx].Members = members
		}
	}
	return nil
}

func (r *apiKeyRepository) attachAggregateMetadataOne(ctx context.Context, key *service.APIKey) error {
	if key == nil {
		return nil
	}
	keys := []service.APIKey{*key}
	if err := r.attachAggregateMetadata(ctx, keys); err != nil {
		return err
	}
	*key = keys[0]
	return nil
}

func (r *apiKeyRepository) serviceAPIKey(ctx context.Context, m *dbent.APIKey) (*service.APIKey, error) {
	key := apiKeyEntityToService(m)
	if err := r.attachAggregateMetadataOne(ctx, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (r *apiKeyRepository) loadKeyKinds(ctx context.Context, ids []int64) (map[int64]string, error) {
	query, args := loadKeyKindsQuery(ids, r.client.Driver().Dialect())
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, err
		}
		out[id] = service.NormalizeAPIKeyKind(kind)
	}
	return out, rows.Err()
}

func (r *apiKeyRepository) loadAggregateMembers(ctx context.Context, aggregateIDs []int64) (map[int64][]*service.APIKey, error) {
	query, args := loadAggregateMemberIDsQuery(aggregateIDs, r.client.Driver().Dialect())
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type membership struct {
		aggregateID int64
		memberID    int64
	}
	var memberships []membership
	memberIDs := make([]int64, 0)
	seenMembers := make(map[int64]struct{})
	for rows.Next() {
		var item membership
		if err := rows.Scan(&item.aggregateID, &item.memberID); err != nil {
			return nil, err
		}
		memberships = append(memberships, item)
		if _, ok := seenMembers[item.memberID]; ok {
			continue
		}
		seenMembers[item.memberID] = struct{}{}
		memberIDs = append(memberIDs, item.memberID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return map[int64][]*service.APIKey{}, nil
	}

	memberKeys, err := r.activeQuery().
		Where(apikey.IDIn(memberIDs...)).
		WithGroup().
		All(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.APIKey, len(memberKeys))
	for i := range memberKeys {
		converted := apiKeyEntityToService(memberKeys[i])
		converted.KeyKind = service.APIKeyKindGroup
		byID[converted.ID] = converted
	}
	out := make(map[int64][]*service.APIKey, len(aggregateIDs))
	for _, item := range memberships {
		member := byID[item.memberID]
		if member == nil {
			continue
		}
		clone := *member
		out[item.aggregateID] = append(out[item.aggregateID], &clone)
	}
	return out, nil
}

func setAPIKeyKindQuery(id int64, kind, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `UPDATE api_keys SET key_kind = $1 WHERE id = $2 AND deleted_at IS NULL`, []any{kind, id}
	}
	return `UPDATE api_keys SET key_kind = ? WHERE id = ? AND deleted_at IS NULL`, []any{kind, id}
}

func deleteAggregateMembersQuery(aggregateID int64, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `DELETE FROM api_key_aggregate_members WHERE aggregate_key_id = $1`, []any{aggregateID}
	}
	return `DELETE FROM api_key_aggregate_members WHERE aggregate_key_id = ?`, []any{aggregateID}
}

func deleteAggregateMembershipsForKeyQuery(keyID int64, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `DELETE FROM api_key_aggregate_members WHERE aggregate_key_id = $1 OR member_key_id = $1`, []any{keyID}
	}
	return `DELETE FROM api_key_aggregate_members WHERE aggregate_key_id = ? OR member_key_id = ?`, []any{keyID, keyID}
}

func listAggregateKeysByMemberIDQuery(memberID int64, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `
			SELECT k.key
			FROM api_key_aggregate_members AS m
			JOIN api_keys AS k ON k.id = m.aggregate_key_id AND k.deleted_at IS NULL
			WHERE m.member_key_id = $1`, []any{memberID}
	}
	return `
		SELECT k.key
		FROM api_key_aggregate_members AS m
		JOIN api_keys AS k ON k.id = m.aggregate_key_id AND k.deleted_at IS NULL
		WHERE m.member_key_id = ?`, []any{memberID}
}

func insertAggregateMembersQuery(aggregateID int64, memberIDs []int64, dialectName string) (string, []any) {
	args := make([]any, 0, 1+len(memberIDs)*2)
	placeholders := make([]string, 0, len(memberIDs))
	if dialectName == dialect.Postgres {
		for i, memberID := range memberIDs {
			placeholders = append(placeholders, fmt.Sprintf("($1, $%d, $%d)", i*2+2, i*2+3))
			args = append(args, memberID, i)
		}
		args = append([]any{aggregateID}, args...)
		return `INSERT INTO api_key_aggregate_members (aggregate_key_id, member_key_id, sort_order) VALUES ` + strings.Join(placeholders, ","), args
	}
	for i, memberID := range memberIDs {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, aggregateID, memberID, i)
	}
	return `INSERT INTO api_key_aggregate_members (aggregate_key_id, member_key_id, sort_order) VALUES ` + strings.Join(placeholders, ","), args
}

func loadKeyKindsQuery(ids []int64, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `SELECT id, key_kind FROM api_keys WHERE id = ANY($1)`, []any{pq.Array(ids)}
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return `SELECT id, key_kind FROM api_keys WHERE id IN (` + strings.Join(placeholders, ",") + `)`, args
}

func loadAggregateMemberIDsQuery(aggregateIDs []int64, dialectName string) (string, []any) {
	if dialectName == dialect.Postgres {
		return `
			SELECT m.aggregate_key_id, m.member_key_id
			FROM api_key_aggregate_members AS m
			JOIN api_keys AS k ON k.id = m.member_key_id AND k.deleted_at IS NULL
			WHERE m.aggregate_key_id = ANY($1)
			ORDER BY m.aggregate_key_id, m.sort_order, m.member_key_id`, []any{pq.Array(aggregateIDs)}
	}
	placeholders := make([]string, len(aggregateIDs))
	args := make([]any, len(aggregateIDs))
	for i, id := range aggregateIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	return `
		SELECT m.aggregate_key_id, m.member_key_id
		FROM api_key_aggregate_members AS m
		JOIN api_keys AS k ON k.id = m.member_key_id AND k.deleted_at IS NULL
		WHERE m.aggregate_key_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY m.aggregate_key_id, m.sort_order, m.member_key_id`, args
}

func isMissingAggregateSchema(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "key_kind") || strings.Contains(msg, "api_key_aggregate_members")
}
