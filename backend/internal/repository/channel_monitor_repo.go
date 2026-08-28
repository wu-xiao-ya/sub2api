package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorhistory"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
)

// channelMonitorRepository ?? service.ChannelMonitorRepository?
//
// ?????
//   - CRUD ? ent?????????????
//   - ?????latest per model / availability???? SQL??? ent ? GROUP BY ?
//     ???????????????
type channelMonitorRepository struct {
	client *dbent.Client
	db     *sql.DB
}

// NewChannelMonitorRepository ???????
func NewChannelMonitorRepository(client *dbent.Client, db *sql.DB) service.ChannelMonitorRepository {
	return &channelMonitorRepository{client: client, db: db}
}

// ---------- CRUD ----------

func (r *channelMonitorRepository) Create(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ChannelMonitor.Create().
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetSourceMode(m.SourceMode).
		SetAPIKeyEncrypted(m.APIKey). // ??????????
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetJitterSeconds(m.JitterSeconds).
		SetRequestTimeoutSeconds(m.RequestTimeoutSeconds).
		SetCreatedBy(m.CreatedBy).
		SetExtraHeaders(channelMonitorHeadersForPersistence(m)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode))
	if m.AccountGroupID != nil {
		builder = builder.SetAccountGroupID(*m.AccountGroupID)
	}
	if m.InternalAPIKeyID != nil {
		builder = builder.SetInternalAPIKeyID(*m.InternalAPIKeyID)
	}
	if m.InternalGroupID != nil {
		builder = builder.SetInternalGroupID(*m.InternalGroupID)
	}
	if m.TemplateID != nil {
		builder = builder.SetTemplateID(*m.TemplateID)
	}
	if m.BodyOverride != nil {
		builder = builder.SetBodyOverride(m.BodyOverride)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.ID = created.ID
	m.CreatedAt = created.CreatedAt
	m.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) FindByDuplicateOperationID(ctx context.Context, operationID string) (*service.ChannelMonitor, error) {
	if strings.TrimSpace(operationID) == "" {
		return nil, nil
	}
	client := clientFromContext(ctx, r.client)
	row, err := client.ChannelMonitor.Query().
		Where(func(selector *entsql.Selector) {
			selector.Where(sqljson.ValueEQ(
				channelmonitor.FieldExtraHeaders,
				operationID,
				sqljson.Path(service.ChannelMonitorDuplicateOperationIDMetadataKey),
			))
		}).
		Order(dbent.Asc(channelmonitor.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find channel monitor duplicate operation: %w", err)
	}
	return entToServiceMonitor(row), nil
}

func (r *channelMonitorRepository) GetByID(ctx context.Context, id int64) (*service.ChannelMonitor, error) {
	row, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return entToServiceMonitor(row), nil
}

func (r *channelMonitorRepository) Update(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	updater := client.ChannelMonitor.UpdateOneID(m.ID).
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetSourceMode(m.SourceMode).
		SetAPIKeyEncrypted(m.APIKey).
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetJitterSeconds(m.JitterSeconds).
		SetRequestTimeoutSeconds(m.RequestTimeoutSeconds).
		SetExtraHeaders(channelMonitorHeadersForPersistence(m)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode))
	if m.AccountGroupID != nil {
		updater = updater.SetAccountGroupID(*m.AccountGroupID)
	} else {
		updater = updater.ClearAccountGroupID()
	}
	if m.InternalAPIKeyID != nil {
		updater = updater.SetInternalAPIKeyID(*m.InternalAPIKeyID)
	} else {
		updater = updater.ClearInternalAPIKeyID()
	}
	if m.InternalGroupID != nil {
		updater = updater.SetInternalGroupID(*m.InternalGroupID)
	} else {
		updater = updater.ClearInternalGroupID()
	}
	if m.TemplateID != nil {
		updater = updater.SetTemplateID(*m.TemplateID)
	} else {
		updater = updater.ClearTemplateID()
	}
	if m.BodyOverride != nil {
		updater = updater.SetBodyOverride(m.BodyOverride)
	} else {
		updater = updater.ClearBodyOverride()
	}

	updated, err := updater.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.UpdatedAt = updated.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.DeleteOneID(id).Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) UpsertLatestImage(ctx context.Context, image *service.ChannelMonitorLatestImage) error {
	if image == nil || image.MonitorID <= 0 || len(image.Data) == 0 {
		return fmt.Errorf("latest image payload is empty")
	}
	const q = `
		INSERT INTO channel_monitor_latest_images (
		    monitor_id, content_type, image_data, generated_at, updated_at
		)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (monitor_id) DO UPDATE SET
		    content_type = EXCLUDED.content_type,
		    image_data = EXCLUDED.image_data,
		    generated_at = EXCLUDED.generated_at,
		    updated_at = NOW()
	`
	if _, err := r.db.ExecContext(
		ctx, q, image.MonitorID, image.ContentType, image.Data, image.GeneratedAt,
	); err != nil {
		return fmt.Errorf("upsert latest image: %w", err)
	}
	return nil
}

func (r *channelMonitorRepository) GetLatestImage(ctx context.Context, monitorID int64) (*service.ChannelMonitorLatestImage, error) {
	const q = `
		SELECT monitor_id, content_type, image_data, generated_at
		FROM channel_monitor_latest_images
		WHERE monitor_id = $1
	`
	image := &service.ChannelMonitorLatestImage{}
	if err := r.db.QueryRowContext(ctx, q, monitorID).Scan(
		&image.MonitorID, &image.ContentType, &image.Data, &image.GeneratedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrChannelMonitorLatestImageNotFound
		}
		return nil, fmt.Errorf("get latest image: %w", err)
	}
	return image, nil
}

func (r *channelMonitorRepository) List(ctx context.Context, params service.ChannelMonitorListParams) ([]*service.ChannelMonitor, int64, error) {
	q := r.client.ChannelMonitor.Query()
	if params.Provider != "" {
		q = q.Where(channelmonitor.ProviderEQ(channelmonitor.Provider(params.Provider)))
	}
	if params.Enabled != nil {
		q = q.Where(channelmonitor.EnabledEQ(*params.Enabled))
	}
	if s := strings.TrimSpace(params.Search); s != "" {
		q = q.Where(channelmonitor.Or(
			channelmonitor.NameContainsFold(s),
			channelmonitor.GroupNameContainsFold(s),
			channelmonitor.PrimaryModelContainsFold(s),
		))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count monitors: %w", err)
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}

	rows, err := q.
		Order(dbent.Desc(channelmonitor.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}

	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, int64(total), nil
}

// ---------- ????? ----------

func (r *channelMonitorRepository) ListEnabled(ctx context.Context) ([]*service.ChannelMonitor, error) {
	rows, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, nil
}

func (r *channelMonitorRepository) MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.UpdateOneID(id).
		SetLastCheckedAt(checkedAt).
		Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) InsertHistoryBatch(ctx context.Context, rows []*service.ChannelMonitorHistoryRow) error {
	if len(rows) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	bulk := make([]*dbent.ChannelMonitorHistoryCreate, 0, len(rows))
	for _, row := range rows {
		c := client.ChannelMonitorHistory.Create().
			SetMonitorID(row.MonitorID).
			SetModel(row.Model).
			SetStatus(channelmonitorhistory.Status(row.Status)).
			SetMessage(row.Message).
			SetCheckedAt(row.CheckedAt)
		if row.LatencyMs != nil {
			c = c.SetLatencyMs(*row.LatencyMs)
		}
		if breakdown := row.LatencyBreakdown.Map(); len(breakdown) > 0 {
			c = c.SetLatencyBreakdown(breakdown)
		}
		if row.PingLatencyMs != nil {
			c = c.SetPingLatencyMs(*row.PingLatencyMs)
		}
		if row.AccountID != nil {
			c = c.SetAccountID(*row.AccountID)
		}
		if row.AccountName != "" {
			c = c.SetAccountName(row.AccountName)
		}
		if row.ProbeMode != "" {
			c = c.SetProbeMode(row.ProbeMode)
		}
		if row.CandidateCount > 0 {
			c = c.SetCandidateCount(row.CandidateCount)
		}
		if row.HealthyCount > 0 {
			c = c.SetHealthyCount(row.HealthyCount)
		}
		bulk = append(bulk, c)
	}
	if _, err := client.ChannelMonitorHistory.CreateBulk(bulk...).Save(ctx); err != nil {
		return fmt.Errorf("insert history bulk: %w", err)
	}
	return nil
}

func (r *channelMonitorRepository) InsertCostEvents(ctx context.Context, events []*service.ChannelMonitorCostEvent) error {
	if len(events) == 0 {
		return nil
	}
	if r.db == nil {
		return fmt.Errorf("channel monitor cost event database is unavailable")
	}
	const q = `
		INSERT INTO channel_monitor_cost_events (
		    monitor_id, account_id, provider, api_mode, model,
		    input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
		    image_count, estimated_cost, account_cost, cost_source, created_at
		)
		VALUES (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8, $9,
		    $10, $11, $12, $13, $14
		)
	`
	for _, event := range events {
		if event == nil || event.MonitorID <= 0 || strings.TrimSpace(event.Model) == "" {
			continue
		}
		if _, err := r.db.ExecContext(
			ctx,
			q,
			event.MonitorID,
			event.AccountID,
			event.Provider,
			event.APIMode,
			event.Model,
			event.InputTokens,
			event.OutputTokens,
			event.CacheCreationTokens,
			event.CacheReadTokens,
			event.ImageCount,
			event.EstimatedCost,
			event.AccountCost,
			event.CostSource,
			event.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert monitor cost event: %w", err)
		}
	}
	return nil
}

// DeleteHistoryBefore ??? checked_at < before ?????? channelMonitorPruneBatchSize ????
// ????????????/WAL ????? (checked_at) ?????? id??? id ??
func (r *channelMonitorRepository) DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneHistorySQL, before)
}

// ListHistory ? checked_at ??????????? N ??????
// model ????????????????????
func (r *channelMonitorRepository) ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*service.ChannelMonitorHistoryEntry, error) {
	q := r.client.ChannelMonitorHistory.Query().
		Where(channelmonitorhistory.MonitorIDEQ(monitorID))
	if strings.TrimSpace(model) != "" {
		q = q.Where(channelmonitorhistory.ModelEQ(model))
	}
	rows, err := q.
		Order(dbent.Desc(channelmonitorhistory.FieldCheckedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	out := make([]*service.ChannelMonitorHistoryEntry, 0, len(rows))
	for _, row := range rows {
		entry := &service.ChannelMonitorHistoryEntry{
			ID:               row.ID,
			Model:            row.Model,
			Status:           string(row.Status),
			LatencyMs:        row.LatencyMs,
			LatencyBreakdown: service.UsageLatencyBreakdownFromMap(row.LatencyBreakdown),
			PingLatencyMs:    row.PingLatencyMs,
			AccountID:        row.AccountID,
			AccountName:      row.AccountName,
			ProbeMode:        row.ProbeMode,
			CandidateCount:   row.CandidateCount,
			HealthyCount:     row.HealthyCount,
			Message:          row.Message,
			CheckedAt:        row.CheckedAt,
		}
		out = append(out, entry)
	}
	return out, nil
}

// GetAccountProbeState reads the durable sticky account state for one model.
func (r *channelMonitorRepository) GetAccountProbeState(ctx context.Context, monitorID int64, model string) (*service.ChannelMonitorAccountProbeState, error) {
	const q = `
		SELECT monitor_id, model, account_id, account_name, final_status,
		       last_latency_ms, last_probe_mode, last_full_sweep_at,
		       last_checked_at, updated_at
		FROM channel_monitor_account_probe_states
		WHERE monitor_id = $1 AND model = $2
	`
	state := &service.ChannelMonitorAccountProbeState{}
	var accountID, latency sql.NullInt64
	var fullSweep sql.NullTime
	if err := r.db.QueryRowContext(ctx, q, monitorID, model).Scan(
		&state.MonitorID, &state.Model, &accountID, &state.AccountName,
		&state.FinalStatus, &latency, &state.LastProbeMode, &fullSweep,
		&state.LastCheckedAt, &state.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get account probe state: %w", err)
	}
	if accountID.Valid {
		id := accountID.Int64
		state.AccountID = &id
	}
	assignNullInt(&state.LastLatencyMs, latency)
	if fullSweep.Valid {
		t := fullSweep.Time
		state.LastFullSweepAt = &t
	}
	return state, nil
}

// ListAccountProbeStates lists current model-level sticky state for admin views.
func (r *channelMonitorRepository) ListAccountProbeStates(ctx context.Context, monitorID int64) ([]*service.ChannelMonitorAccountProbeState, error) {
	const q = `
		SELECT monitor_id, model, account_id, account_name, final_status,
		       last_latency_ms, last_probe_mode, last_full_sweep_at,
		       last_checked_at, updated_at
		FROM channel_monitor_account_probe_states
		WHERE monitor_id = $1
		ORDER BY model
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID)
	if err != nil {
		return nil, fmt.Errorf("list account probe states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*service.ChannelMonitorAccountProbeState, 0)
	for rows.Next() {
		state := &service.ChannelMonitorAccountProbeState{}
		var accountID, latency sql.NullInt64
		var fullSweep sql.NullTime
		if err := rows.Scan(
			&state.MonitorID, &state.Model, &accountID, &state.AccountName,
			&state.FinalStatus, &latency, &state.LastProbeMode, &fullSweep,
			&state.LastCheckedAt, &state.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan account probe state: %w", err)
		}
		if accountID.Valid {
			id := accountID.Int64
			state.AccountID = &id
		}
		assignNullInt(&state.LastLatencyMs, latency)
		if fullSweep.Valid {
			t := fullSweep.Time
			state.LastFullSweepAt = &t
		}
		out = append(out, state)
	}
	return out, rows.Err()
}

// UpsertAccountProbeState persists one model's selected account and final state.
func (r *channelMonitorRepository) UpsertAccountProbeState(ctx context.Context, state *service.ChannelMonitorAccountProbeState) error {
	if state == nil || state.MonitorID <= 0 || strings.TrimSpace(state.Model) == "" {
		return fmt.Errorf("account probe state is empty")
	}
	const q = `
		INSERT INTO channel_monitor_account_probe_states (
		    monitor_id, model, account_id, account_name, final_status,
		    last_latency_ms, last_probe_mode, last_full_sweep_at,
		    last_checked_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (monitor_id, model) DO UPDATE SET
		    account_id = EXCLUDED.account_id,
		    account_name = EXCLUDED.account_name,
		    final_status = EXCLUDED.final_status,
		    last_latency_ms = EXCLUDED.last_latency_ms,
		    last_probe_mode = EXCLUDED.last_probe_mode,
		    last_full_sweep_at = COALESCE(
		        EXCLUDED.last_full_sweep_at,
		        channel_monitor_account_probe_states.last_full_sweep_at
		    ),
		    last_checked_at = EXCLUDED.last_checked_at,
		    updated_at = NOW()
	`
	var accountID any
	if state.AccountID != nil {
		accountID = *state.AccountID
	}
	var latency any
	if state.LastLatencyMs != nil {
		latency = *state.LastLatencyMs
	}
	var fullSweep any
	if state.LastFullSweepAt != nil {
		fullSweep = *state.LastFullSweepAt
	}
	if _, err := r.db.ExecContext(ctx, q,
		state.MonitorID, state.Model, accountID, state.AccountName,
		state.FinalStatus, latency, state.LastProbeMode, fullSweep,
		state.LastCheckedAt,
	); err != nil {
		return fmt.Errorf("upsert account probe state: %w", err)
	}
	return nil
}

func (r *channelMonitorRepository) ClearAccountProbeStates(ctx context.Context, monitorID int64) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM channel_monitor_account_probe_states WHERE monitor_id = $1`,
		monitorID,
	); err != nil {
		return fmt.Errorf("clear account probe states: %w", err)
	}
	return nil
}

// ---------- ????????? SQL? ----------

// ListLatestPerModel ? DISTINCT ON ??? (monitor_id, model) ????????
// ?? (monitor_id, model, checked_at DESC) ???? Index Scan?
func (r *channelMonitorRepository) ListLatestPerModel(ctx context.Context, monitorID int64) ([]*service.ChannelMonitorLatest, error) {
	const q = `
		SELECT DISTINCT ON (model)
		    model, status, latency_ms, latency_breakdown, ping_latency_ms, account_id, account_name, probe_mode,
		    candidate_count, healthy_count, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id = $1
		ORDER BY model, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID)
	if err != nil {
		return nil, fmt.Errorf("query latest per model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorLatest, 0)
	for rows.Next() {
		l := &service.ChannelMonitorLatest{}
		var latency, ping, accountID sql.NullInt64
		var latencyBreakdown sql.NullString
		if err := rows.Scan(
			&l.Model, &l.Status, &latency, &latencyBreakdown, &ping, &accountID, &l.AccountName, &l.ProbeMode,
			&l.CandidateCount, &l.HealthyCount, &l.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan latest row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		l.LatencyBreakdown = usageLatencyBreakdownFromNullJSON(latencyBreakdown)
		assignNullInt(&l.PingLatencyMs, ping)
		assignNullInt64(&l.AccountID, accountID)
		out = append(out, l)
	}
	return out, rows.Err()
}

// assignNullInt ? sql.NullInt64 ??? *int ?????valid ???? int??
// ?????? latency / ping ???? if latency.Valid { v := int(...) ... } ???
func assignNullInt(dst **int, n sql.NullInt64) {
	if !n.Valid {
		return
	}
	v := int(n.Int64)
	*dst = &v
}

func assignNullInt64(dst **int64, n sql.NullInt64) {
	if !n.Valid {
		return
	}
	v := n.Int64
	*dst = &v
}

// ComputeAvailability ?????????????????????
// "??" = status IN (operational, degraded)?
//
// ??????????? 1 ??????????????
// ???? 30 ??monitorHistoryRetentionDays???? <= 30 ????? histories?
// ??????????? UNION ??? UTC ???????
func (r *channelMonitorRepository) ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*service.ChannelMonitorAvailability, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	const q = `
		SELECT model,
		       COUNT(*)                                                             AS total,
		       COUNT(*) FILTER (WHERE status IN ('operational','degraded'))         AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL)::float8 / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = $1
		  AND checked_at >= NOW() - ($2::int || ' days')::interval
		GROUP BY model
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query availability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorAvailability, 0)
	for rows.Next() {
		row, err := scanAvailabilityRow(rows, windowDays)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// scanAvailabilityRow ??? (model, total, ok, avg_latency) ??? ChannelMonitorAvailability?
// ???? ComputeAvailability?4 ???????????? monitor_id ?? inline ? finalizeAvailabilityRow?
func scanAvailabilityRow(rows interface{ Scan(...any) error }, windowDays int) (*service.ChannelMonitorAvailability, error) {
	row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
	var avgLatency sql.NullFloat64
	if err := rows.Scan(&row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
		return nil, fmt.Errorf("scan availability row: %w", err)
	}
	finalizeAvailabilityRow(row, avgLatency)
	return row, nil
}

// finalizeAvailabilityRow ?? OperationalChecks/TotalChecks ??????
// ?? sql.NullFloat64 ???????? *int????????????
func finalizeAvailabilityRow(row *service.ChannelMonitorAvailability, avgLatency sql.NullFloat64) {
	if row.TotalChecks > 0 {
		row.AvailabilityPct = float64(row.OperationalChecks) * 100.0 / float64(row.TotalChecks)
	}
	if avgLatency.Valid {
		v := int(avgLatency.Float64)
		row.AvgLatencyMs = &v
	}
}

// ListLatestForMonitorIDs ??????????"?? (monitor_id, model) ????"???
// ?? PG ? DISTINCT ON ????? (monitor_id, model, checked_at DESC) ???? Index Scan?
func (r *channelMonitorRepository) ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*service.ChannelMonitorLatest, error) {
	out := make(map[int64][]*service.ChannelMonitorLatest, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const q = `
		SELECT DISTINCT ON (monitor_id, model)
		    monitor_id, model, status, latency_ms, latency_breakdown, ping_latency_ms, account_id, account_name, probe_mode,
		    candidate_count, healthy_count, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id = ANY($1)
		ORDER BY monitor_id, model, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query latest batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		l := &service.ChannelMonitorLatest{}
		var latency, ping, accountID sql.NullInt64
		var latencyBreakdown sql.NullString
		if err := rows.Scan(
			&monitorID, &l.Model, &l.Status, &latency, &latencyBreakdown, &ping, &accountID, &l.AccountName, &l.ProbeMode,
			&l.CandidateCount, &l.HealthyCount, &l.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan latest batch row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		l.LatencyBreakdown = usageLatencyBreakdownFromNullJSON(latencyBreakdown)
		assignNullInt(&l.PingLatencyMs, ping)
		assignNullInt64(&l.AccountID, accountID)
		out[monitorID] = append(out[monitorID], l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListRecentHistoryForMonitors ??? monitor ?????"????"?? N ????? checked_at DESC???????
// primaryModels[monitorID] ?????????????monitor ?? primaryModels ????????
// ?? CTE + unnest(?? int8/text ??) ?? (monitor_id, model) ????
// ?? ROW_NUMBER() OVER (PARTITION BY monitor_id) ???? N ??
//
// ????map[monitorID] -> []*ChannelMonitorHistoryEntry??? message?????????
// ? ids / ? primaryModels ??? map?????
func (r *channelMonitorRepository) ListRecentHistoryForMonitors(
	ctx context.Context,
	ids []int64,
	primaryModels map[int64]string,
	perMonitorLimit int,
) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	out := make(map[int64][]*service.ChannelMonitorHistoryEntry, len(ids))
	pairIDs, pairModels := buildMonitorModelPairs(ids, primaryModels)
	if len(pairIDs) == 0 {
		return out, nil
	}
	perMonitorLimit = clampTimelineLimit(perMonitorLimit)

	const q = `
		WITH targets AS (
		    SELECT unnest($1::bigint[]) AS monitor_id,
		           unnest($2::text[])   AS model
		),
		ranked AS (
		    SELECT h.monitor_id,
		           h.status,
		           h.latency_ms,
		           h.ping_latency_ms,
		           h.checked_at,
		           ROW_NUMBER() OVER (PARTITION BY h.monitor_id ORDER BY h.checked_at DESC) AS rn
		    FROM channel_monitor_histories h
		    JOIN targets t
		      ON t.monitor_id = h.monitor_id AND t.model = h.model
		)
		SELECT monitor_id, status, latency_ms, ping_latency_ms, checked_at
		FROM ranked
		WHERE rn <= $3
		ORDER BY monitor_id, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(pairIDs), pq.Array(pairModels), perMonitorLimit)
	if err != nil {
		return nil, fmt.Errorf("query recent history batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		entry := &service.ChannelMonitorHistoryEntry{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&monitorID, &entry.Status, &latency, &ping, &entry.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan recent history row: %w", err)
		}
		assignNullInt(&entry.LatencyMs, latency)
		assignNullInt(&entry.PingLatencyMs, ping)
		out[monitorID] = append(out[monitorID], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildMonitorModelPairs ?? ids ?????? (monitor_id, model) ??model ??????
// ????????????????? unnest ???
func buildMonitorModelPairs(ids []int64, primaryModels map[int64]string) ([]int64, []string) {
	if len(ids) == 0 || len(primaryModels) == 0 {
		return nil, nil
	}
	pairIDs := make([]int64, 0, len(ids))
	pairModels := make([]string, 0, len(ids))
	for _, id := range ids {
		model, ok := primaryModels[id]
		if !ok || strings.TrimSpace(model) == "" {
			continue
		}
		pairIDs = append(pairIDs, id)
		pairModels = append(pairModels, model)
	}
	return pairIDs, pairModels
}

// timelineLimit* ?? timeline ??? perMonitorLimit ?????
// ?? 1 ????????????? 200 ???????? SQL ?????ROW_NUMBER ??????
const (
	timelineLimitMin = 1
	timelineLimitMax = 200
)

// clampTimelineLimit ? perMonitorLimit ??? [timelineLimitMin, timelineLimitMax]????????????
func clampTimelineLimit(n int) int {
	if n < timelineLimitMin {
		return timelineLimitMin
	}
	if n > timelineLimitMax {
		return timelineLimitMax
	}
	return n
}

// ComputeAvailabilityForMonitors ????????????????????????????
// ???? 30 ????? histories??? <= 30 ????????
func (r *channelMonitorRepository) ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*service.ChannelMonitorAvailability, error) {
	out := make(map[int64][]*service.ChannelMonitorAvailability, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if windowDays <= 0 {
		windowDays = 7
	}
	const q = `
		SELECT monitor_id,
		       model,
		       COUNT(*)                                                             AS total,
		       COUNT(*) FILTER (WHERE status IN ('operational','degraded'))         AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL)::float8 / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = ANY($1)
		  AND checked_at >= NOW() - ($2::int || ' days')::interval
		GROUP BY monitor_id, model
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(ids), windowDays)
	if err != nil {
		return nil, fmt.Errorf("query availability batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
		var avgLatency sql.NullFloat64
		if err := rows.Scan(&monitorID, &row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
			return nil, fmt.Errorf("scan availability batch row: %w", err)
		}
		// ???????? monitor_id?????????/???????? monitor ?????
		// ?? finalizeAvailabilityRow ?????????????? NullFloat ???
		finalizeAvailabilityRow(row, avgLatency)
		out[monitorID] = append(out[monitorID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- ???? ----------

// UpsertDailyRollupsFor ? targetDate ???[targetDate, targetDate+1d)????
// ? (monitor_id, model, bucket_date) ???? channel_monitor_daily_rollups?
//   - ? ON CONFLICT (monitor_id, model, bucket_date) DO UPDATE ???????
//     ??????????????
//   - $1::date ? PG ????? truncate ? UTC ???????????? targetDate?
func (r *channelMonitorRepository) UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error) {
	const q = `
		INSERT INTO channel_monitor_daily_rollups (
		    monitor_id, model, bucket_date,
		    total_checks, ok_count,
		    operational_count, degraded_count, failed_count, error_count,
		    sum_latency_ms, count_latency,
		    sum_ping_latency_ms, count_ping_latency,
		    computed_at
		)
		SELECT
		    monitor_id,
		    model,
		    $1::date AS bucket_date,
		    COUNT(*)                                                         AS total_checks,
		    COUNT(*) FILTER (WHERE status IN ('operational','degraded'))     AS ok_count,
		    COUNT(*) FILTER (WHERE status = 'operational')                   AS operational_count,
		    COUNT(*) FILTER (WHERE status = 'degraded')                      AS degraded_count,
		    COUNT(*) FILTER (WHERE status = 'failed')                        AS failed_count,
		    COUNT(*) FILTER (WHERE status = 'error')                         AS error_count,
		    COALESCE(SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL), 0)             AS sum_latency_ms,
		    COUNT(latency_ms)                                                AS count_latency,
		    COALESCE(SUM(ping_latency_ms) FILTER (WHERE ping_latency_ms IS NOT NULL), 0)   AS sum_ping_latency_ms,
		    COUNT(ping_latency_ms)                                           AS count_ping_latency,
		    NOW()
		FROM channel_monitor_histories
		WHERE checked_at >= $1::date
		  AND checked_at <  ($1::date + INTERVAL '1 day')
		GROUP BY monitor_id, model
		ON CONFLICT (monitor_id, model, bucket_date) DO UPDATE SET
		    total_checks        = EXCLUDED.total_checks,
		    ok_count            = EXCLUDED.ok_count,
		    operational_count   = EXCLUDED.operational_count,
		    degraded_count      = EXCLUDED.degraded_count,
		    failed_count        = EXCLUDED.failed_count,
		    error_count         = EXCLUDED.error_count,
		    sum_latency_ms      = EXCLUDED.sum_latency_ms,
		    count_latency       = EXCLUDED.count_latency,
		    sum_ping_latency_ms = EXCLUDED.sum_ping_latency_ms,
		    count_ping_latency  = EXCLUDED.count_ping_latency,
		    computed_at         = NOW()
	`
	res, err := r.db.ExecContext(ctx, q, targetDate)
	if err != nil {
		return 0, fmt.Errorf("upsert daily rollups for %s: %w", targetDate.Format("2006-01-02"), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected (upsert rollups): %w", err)
	}
	return n, nil
}

// DeleteRollupsBefore ??? bucket_date < beforeDate ??????????
func (r *channelMonitorRepository) DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneRollupSQL, beforeDate)
}

// channelMonitorPruneBatchSize ???????? ops_cleanup_service ????? 5000?
// ????? id ??????????? WAL ???
const channelMonitorPruneBatchSize = 5000

// channelMonitorPruneHistorySQL ????????????
const channelMonitorPruneHistorySQL = `
WITH batch AS (
    SELECT id FROM channel_monitor_histories
    WHERE checked_at < $1
    ORDER BY id
    LIMIT $2
)
DELETE FROM channel_monitor_histories
WHERE id IN (SELECT id FROM batch)
`

// channelMonitorPruneRollupSQL ????? rollup ?????bucket_date ?? ::date ??
// ??? DATE ??????
const channelMonitorPruneRollupSQL = `
WITH batch AS (
    SELECT id FROM channel_monitor_daily_rollups
    WHERE bucket_date < $1::date
    ORDER BY id
    LIMIT $2
)
DELETE FROM channel_monitor_daily_rollups
WHERE id IN (SELECT id FROM batch)
`

// deleteChannelMonitorBatched ?????? DELETE??????? 0??????????
// cutoff ?????????????? time.Time ? TIMESTAMPTZ?rollup ? time.Time SQL ? ::date ????
func deleteChannelMonitorBatched(ctx context.Context, db *sql.DB, query string, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res, err := db.ExecContext(ctx, query, cutoff, channelMonitorPruneBatchSize)
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune batch: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune rows affected: %w", err)
		}
		total += affected
		if affected == 0 {
			break
		}
	}
	return total, nil
}

// LoadAggregationWatermark ? watermark ??id=1??
// watermark ??? ent schema???????????? SQL?
//   - ????? last_aggregated_date IS NULL??? (nil, nil)?????????????
func (r *channelMonitorRepository) LoadAggregationWatermark(ctx context.Context) (*time.Time, error) {
	const q = `SELECT last_aggregated_date FROM channel_monitor_aggregation_watermark WHERE id = 1`
	var t sql.NullTime
	if err := r.db.QueryRowContext(ctx, q).Scan(&t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load aggregation watermark: %w", err)
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

// UpdateAggregationWatermark ?? watermark?UPSERT ? id=1??
// $1::date ? PG ??? truncate ? UTC ???? last_aggregated_date ?? DATE ?????
func (r *channelMonitorRepository) UpdateAggregationWatermark(ctx context.Context, date time.Time) error {
	const q = `
		INSERT INTO channel_monitor_aggregation_watermark (id, last_aggregated_date, updated_at)
		VALUES (1, $1::date, NOW())
		ON CONFLICT (id) DO UPDATE SET
		    last_aggregated_date = EXCLUDED.last_aggregated_date,
		    updated_at           = NOW()
	`
	if _, err := r.db.ExecContext(ctx, q, date); err != nil {
		return fmt.Errorf("update aggregation watermark: %w", err)
	}
	return nil
}

// ---------- helpers ----------

func entToServiceMonitor(row *dbent.ChannelMonitor) *service.ChannelMonitor {
	if row == nil {
		return nil
	}
	extras := row.ExtraModels
	if extras == nil {
		extras = []string{}
	}
	headers := make(map[string]string, len(row.ExtraHeaders))
	for key, value := range row.ExtraHeaders {
		headers[key] = value
	}
	duplicateOperationID := headers[service.ChannelMonitorDuplicateOperationIDMetadataKey]
	delete(headers, service.ChannelMonitorDuplicateOperationIDMetadataKey)
	out := &service.ChannelMonitor{
		ID:                    row.ID,
		Name:                  row.Name,
		Provider:              string(row.Provider),
		APIMode:               defaultAPIModeRepo(row.APIMode),
		Endpoint:              row.Endpoint,
		APIKey:                row.APIKeyEncrypted, // ?????service ?????
		SourceMode:            service.NormalizeMonitorSourceModeForAPI(row.SourceMode),
		InternalAPIKeyID:      row.InternalAPIKeyID,
		InternalGroupID:       row.InternalGroupID,
		PrimaryModel:          row.PrimaryModel,
		ExtraModels:           extras,
		GroupName:             row.GroupName,
		AccountGroupID:        row.AccountGroupID,
		Enabled:               row.Enabled,
		IntervalSeconds:       row.IntervalSeconds,
		JitterSeconds:         row.JitterSeconds,
		RequestTimeoutSeconds: row.RequestTimeoutSeconds,
		LastCheckedAt:         row.LastCheckedAt,
		CreatedBy:             row.CreatedBy,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		ExtraHeaders:          headers,
		BodyOverrideMode:      row.BodyOverrideMode,
		BodyOverride:          row.BodyOverride,
		DuplicateOperationID:  duplicateOperationID,
	}
	if row.TemplateID != nil {
		id := *row.TemplateID
		out.TemplateID = &id
	}
	return out
}

func channelMonitorHeadersForPersistence(m *service.ChannelMonitor) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	headers := make(map[string]string, len(m.ExtraHeaders)+1)
	for key, value := range m.ExtraHeaders {
		if key == service.ChannelMonitorDuplicateOperationIDMetadataKey {
			continue
		}
		headers[key] = value
	}
	if operationID := strings.TrimSpace(m.DuplicateOperationID); operationID != "" {
		headers[service.ChannelMonitorDuplicateOperationIDMetadataKey] = operationID
	}
	return headers
}

// emptyHeadersIfNilRepo ? service.emptyHeadersIfNil ?????
// repo ?????? import ???
func emptyHeadersIfNilRepo(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

// defaultBodyModeRepo ????? off????????
func defaultBodyModeRepo(mode string) string {
	if mode == "" {
		return "off"
	}
	return mode
}

func defaultAPIModeRepo(apiMode string) string {
	if apiMode == "" {
		return "chat_completions"
	}
	return apiMode
}

func emptySliceIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
