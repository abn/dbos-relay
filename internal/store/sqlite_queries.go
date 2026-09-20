package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

type sqliteDBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sqliteQueries struct {
	db sqliteDBTX
}

var _ gen.Querier = (*sqliteQueries)(nil)

func newSQLiteQueries(db sqliteDBTX) *sqliteQueries {
	return &sqliteQueries{db: db}
}

func scanOrganisation(idStr string, name string, retention int64, createdStr string) gen.Organisation {
	var o gen.Organisation
	o.ID = textToUUID(idStr)
	o.Name = name
	o.AuditLogRetentionDays = int32(retention)
	o.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return o
}

// ----------------------------------------------------------------------
// Organisations
// ----------------------------------------------------------------------

func (q *sqliteQueries) CreateOrganisation(ctx context.Context, name string) (gen.Organisation, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO organisations (id, name) VALUES (?, ?) RETURNING id, name, audit_log_retention_days, created_at`,
		id, name,
	)
	var idStr, nameStr, createdStr string
	var retention int64
	if err := row.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
		return gen.Organisation{}, mapDBErr(err)
	}
	return scanOrganisation(idStr, nameStr, retention, createdStr), nil
}

func (q *sqliteQueries) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, name, audit_log_retention_days, created_at FROM organisations WHERE name = ?`,
		name,
	)
	var idStr, nameStr, createdStr string
	var retention int64
	if err := row.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
		return gen.Organisation{}, mapDBErr(err)
	}
	return scanOrganisation(idStr, nameStr, retention, createdStr), nil
}

func (q *sqliteQueries) GetOrganisationByID(ctx context.Context, id pgtype.UUID) (gen.Organisation, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, name, audit_log_retention_days, created_at FROM organisations WHERE id = ?`,
		uuidToText(id),
	)
	var idStr, nameStr, createdStr string
	var retention int64
	if err := row.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
		return gen.Organisation{}, mapDBErr(err)
	}
	return scanOrganisation(idStr, nameStr, retention, createdStr), nil
}

func (q *sqliteQueries) UpsertOrganisation(ctx context.Context, name string) (gen.Organisation, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO organisations (id, name) VALUES (?, ?)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id, name, audit_log_retention_days, created_at`,
		id, name,
	)
	var idStr, nameStr, createdStr string
	var retention int64
	if err := row.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
		return gen.Organisation{}, mapDBErr(err)
	}
	return scanOrganisation(idStr, nameStr, retention, createdStr), nil
}

func (q *sqliteQueries) ListAllOrganisations(ctx context.Context) ([]gen.Organisation, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, name, audit_log_retention_days, created_at FROM organisations ORDER BY name`)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()
	var out []gen.Organisation
	for rows.Next() {
		var idStr, nameStr, createdStr string
		var retention int64
		if err := rows.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
			return nil, err
		}
		out = append(out, scanOrganisation(idStr, nameStr, retention, createdStr))
	}
	return out, mapDBErr(rows.Err())
}

func (q *sqliteQueries) UpdateOrganisation(ctx context.Context, arg gen.UpdateOrganisationParams) (gen.Organisation, error) {
	var nameVal *string
	if arg.Name != nil {
		nameVal = arg.Name
	}
	var retentionVal *int32
	if arg.AuditLogRetentionDays != nil {
		retentionVal = arg.AuditLogRetentionDays
	}
	row := q.db.QueryRowContext(ctx,
		`UPDATE organisations SET
			name = COALESCE(?, name),
			audit_log_retention_days = COALESCE(?, audit_log_retention_days)
		 WHERE id = ?
		 RETURNING id, name, audit_log_retention_days, created_at`,
		nameVal, retentionVal, uuidToText(arg.ID),
	)
	var idStr, nameStr, createdStr string
	var retention int64
	if err := row.Scan(&idStr, &nameStr, &retention, &createdStr); err != nil {
		return gen.Organisation{}, mapDBErr(err)
	}
	return scanOrganisation(idStr, nameStr, retention, createdStr), nil
}

// ----------------------------------------------------------------------
// Applications
// ----------------------------------------------------------------------

func (q *sqliteQueries) CreateApplication(ctx context.Context, arg gen.CreateApplicationParams) (gen.Application, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	settings := string(arg.Settings)
	if settings == "" {
		settings = "{}"
	}
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO applications (id, organisation_id, name, settings) VALUES (?, ?, ?, ?)
		 RETURNING id, organisation_id, name, settings, created_at`,
		id, uuidToText(arg.OrganisationID), arg.Name, settings,
	)
	return scanApplication(row)
}

func (q *sqliteQueries) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, organisation_id, name, settings, created_at
		 FROM applications WHERE organisation_id = ? AND name = ?`,
		uuidToText(arg.OrganisationID), arg.Name,
	)
	return scanApplication(row)
}

func (q *sqliteQueries) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, organisation_id, name, settings, created_at
		 FROM applications WHERE id = ?`,
		uuidToText(id),
	)
	return scanApplication(row)
}

func (q *sqliteQueries) ListApplicationsByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]gen.Application, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, name, settings, created_at
		 FROM applications WHERE organisation_id = ?
		 ORDER BY created_at DESC`,
		uuidToText(organisationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var apps []gen.Application
	for rows.Next() {
		app, err := scanApplicationRows(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, mapDBErr(rows.Err())
}

func (q *sqliteQueries) UpsertApplication(ctx context.Context, arg gen.UpsertApplicationParams) (gen.Application, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	settings := string(arg.Settings)
	if settings == "" {
		settings = "{}"
	}
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO applications (id, organisation_id, name, settings) VALUES (?, ?, ?, ?)
		 ON CONFLICT (organisation_id, name) DO UPDATE SET settings = EXCLUDED.settings
		 RETURNING id, organisation_id, name, settings, created_at`,
		id, uuidToText(arg.OrganisationID), arg.Name, settings,
	)
	return scanApplication(row)
}

func (q *sqliteQueries) UpdateApplicationSettings(ctx context.Context, arg gen.UpdateApplicationSettingsParams) (gen.Application, error) {
	settings := string(arg.Settings)
	if settings == "" {
		settings = "{}"
	}
	row := q.db.QueryRowContext(ctx,
		`UPDATE applications SET settings = ?
		 WHERE organisation_id = ? AND name = ?
		 RETURNING id, organisation_id, name, settings, created_at`,
		settings, uuidToText(arg.OrganisationID), arg.Name,
	)
	return scanApplication(row)
}

func (q *sqliteQueries) DeleteApplication(ctx context.Context, arg gen.DeleteApplicationParams) (gen.Application, error) {
	row := q.db.QueryRowContext(ctx,
		`DELETE FROM applications WHERE organisation_id = ? AND name = ?
		 RETURNING id, organisation_id, name, settings, created_at`,
		uuidToText(arg.OrganisationID), arg.Name,
	)
	return scanApplication(row)
}

func (q *sqliteQueries) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, name, settings, created_at
		 FROM applications ORDER BY name ASC`,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var apps []gen.Application
	for rows.Next() {
		app, err := scanApplicationRows(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, mapDBErr(rows.Err())
}

func scanApplication(row *sql.Row) (gen.Application, error) {
	var a gen.Application
	var idStr, orgStr, settStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &a.Name, &settStr, &createdStr); err != nil {
		return a, mapDBErr(err)
	}
	a.ID = textToUUID(idStr)
	a.OrganisationID = textToUUID(orgStr)
	a.Settings = []byte(settStr)
	a.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return a, nil
}

func scanApplicationRows(rows *sql.Rows) (gen.Application, error) {
	var a gen.Application
	var idStr, orgStr, settStr, createdStr string
	if err := rows.Scan(&idStr, &orgStr, &a.Name, &settStr, &createdStr); err != nil {
		return a, mapDBErr(err)
	}
	a.ID = textToUUID(idStr)
	a.OrganisationID = textToUUID(orgStr)
	a.Settings = []byte(settStr)
	a.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return a, nil
}

// ----------------------------------------------------------------------
// Instances
// ----------------------------------------------------------------------

func (q *sqliteQueries) UpsertInstance(ctx context.Context, arg gen.UpsertInstanceParams) (gen.Instance, error) {
	idStr := uuidToText(arg.ID)
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO instances (id, advertise_address, port, started_at, heartbeat_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		     advertise_address = EXCLUDED.advertise_address,
		     port = EXCLUDED.port,
		     heartbeat_at = EXCLUDED.heartbeat_at
		 RETURNING id, advertise_address, port, started_at, heartbeat_at`,
		idStr, arg.AdvertiseAddress, arg.Port, nowStr, nowStr,
	)
	var inst gen.Instance
	var retID, startStr, hbStr string
	if err := row.Scan(&retID, &inst.AdvertiseAddress, &inst.Port, &startStr, &hbStr); err != nil {
		return inst, mapDBErr(err)
	}
	inst.ID = textToUUID(retID)
	inst.StartedAt = textToTimestamptz(sql.NullString{String: startStr, Valid: true})
	inst.HeartbeatAt = textToTimestamptz(sql.NullString{String: hbStr, Valid: true})
	return inst, nil
}

func (q *sqliteQueries) HeartbeatInstance(ctx context.Context, id pgtype.UUID) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := q.db.ExecContext(ctx,
		`UPDATE instances SET heartbeat_at = ? WHERE id = ?`,
		nowStr, uuidToText(id),
	)
	return mapDBErr(err)
}

func (q *sqliteQueries) GetInstance(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, advertise_address, port, started_at, heartbeat_at FROM instances WHERE id = ?`,
		uuidToText(id),
	)
	var inst gen.Instance
	var retID, startStr, hbStr string
	if err := row.Scan(&retID, &inst.AdvertiseAddress, &inst.Port, &startStr, &hbStr); err != nil {
		return inst, mapDBErr(err)
	}
	inst.ID = textToUUID(retID)
	inst.StartedAt = textToTimestamptz(sql.NullString{String: startStr, Valid: true})
	inst.HeartbeatAt = textToTimestamptz(sql.NullString{String: hbStr, Valid: true})
	return inst, nil
}

func (q *sqliteQueries) ListHealthyInstances(ctx context.Context, heartbeatAt pgtype.Timestamptz) ([]gen.Instance, error) {
	hbText := timestamptzToText(heartbeatAt)
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, advertise_address, port, started_at, heartbeat_at FROM instances
		 WHERE heartbeat_at > ? ORDER BY started_at ASC`,
		hbText,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Instance
	for rows.Next() {
		var inst gen.Instance
		var retID, startStr, hbStr string
		if err := rows.Scan(&retID, &inst.AdvertiseAddress, &inst.Port, &startStr, &hbStr); err != nil {
			return nil, err
		}
		inst.ID = textToUUID(retID)
		inst.StartedAt = textToTimestamptz(sql.NullString{String: startStr, Valid: true})
		inst.HeartbeatAt = textToTimestamptz(sql.NullString{String: hbStr, Valid: true})
		list = append(list, inst)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) DeleteInstance(ctx context.Context, id pgtype.UUID) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, uuidToText(id))
	return mapDBErr(err)
}

func (q *sqliteQueries) DeleteStaleInstances(ctx context.Context, heartbeatAt pgtype.Timestamptz) (int64, error) {
	hbText := timestamptzToText(heartbeatAt)
	res, err := q.db.ExecContext(ctx, `DELETE FROM instances WHERE heartbeat_at < ?`, hbText)
	if err != nil {
		return 0, mapDBErr(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ----------------------------------------------------------------------
// Executors
// ----------------------------------------------------------------------

func (q *sqliteQueries) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	meta := string(arg.Metadata)
	if meta == "" {
		meta = "{}"
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	var ownerID *string
	if arg.OwnerInstanceID.Valid {
		s := uuidToText(arg.OwnerInstanceID)
		ownerID = &s
	}
	leaseStr := timestamptzToText(arg.LeaseExpiresAt)

	row := q.db.QueryRowContext(ctx,
		`INSERT INTO executors (
		    id, application_id, executor_id, application_version, hostname, metadata,
		    status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at
		 ) VALUES (?, ?, ?, ?, ?, ?, 'connected', ?, ?, ?, ?, NULL)
		 ON CONFLICT (application_id, executor_id) DO UPDATE SET
		     application_version = EXCLUDED.application_version,
		     hostname = EXCLUDED.hostname,
		     metadata = EXCLUDED.metadata,
		     status = 'connected',
		     owner_instance_id = EXCLUDED.owner_instance_id,
		     lease_expires_at = EXCLUDED.lease_expires_at,
		     last_seen_at = EXCLUDED.last_seen_at,
		     disconnected_at = NULL
		 RETURNING id, application_id, executor_id, application_version, hostname, metadata,
		           status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at`,
		id, uuidToText(arg.ApplicationID), arg.ExecutorID, arg.ApplicationVersion, arg.Hostname, meta,
		ownerID, leaseStr, nowStr, nowStr,
	)
	return scanExecutor(row)
}

func (q *sqliteQueries) TouchExecutorLastSeen(ctx context.Context, arg gen.TouchExecutorLastSeenParams) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	leaseStr := timestamptzToText(arg.LeaseExpiresAt)
	_, err := q.db.ExecContext(ctx,
		`UPDATE executors SET last_seen_at = ?, lease_expires_at = ?
		 WHERE application_id = ? AND executor_id = ?`,
		nowStr, leaseStr, uuidToText(arg.ApplicationID), arg.ExecutorID,
	)
	return mapDBErr(err)
}

func (q *sqliteQueries) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	row := q.db.QueryRowContext(ctx,
		`UPDATE executors SET status = 'disconnected', disconnected_at = ?, owner_instance_id = NULL, lease_expires_at = NULL
		 WHERE application_id = ? AND executor_id = ?
		 RETURNING id, application_id, executor_id, application_version, hostname, metadata,
		           status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at`,
		nowStr, uuidToText(arg.ApplicationID), arg.ExecutorID,
	)
	return scanExecutor(row)
}

func (q *sqliteQueries) ListExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, application_id, executor_id, application_version, hostname, metadata,
		        status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at
		 FROM executors WHERE application_id = ? ORDER BY connected_at DESC`,
		uuidToText(applicationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Executor
	for rows.Next() {
		e, err := scanExecutorRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) ListConnectedExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, application_id, executor_id, application_version, hostname, metadata,
		        status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at
		 FROM executors WHERE application_id = ? AND status = 'connected' ORDER BY last_seen_at DESC`,
		uuidToText(applicationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Executor
	for rows.Next() {
		e, err := scanExecutorRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetExecutorByID(ctx context.Context, arg gen.GetExecutorByIDParams) (gen.Executor, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, application_id, executor_id, application_version, hostname, metadata,
		        status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at
		 FROM executors WHERE application_id = ? AND executor_id = ?`,
		uuidToText(arg.ApplicationID), arg.ExecutorID,
	)
	return scanExecutor(row)
}

func (q *sqliteQueries) ReapExpiredExecutors(ctx context.Context, disconnectedAt pgtype.Timestamptz) (int64, error) {
	dtStr := timestamptzToText(disconnectedAt)
	res, err := q.db.ExecContext(ctx,
		`UPDATE executors SET status = 'dead'
		 WHERE status = 'disconnected' AND disconnected_at < ?`,
		dtStr,
	)
	if err != nil {
		return 0, mapDBErr(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (q *sqliteQueries) SetExecutorDead(ctx context.Context, arg gen.SetExecutorDeadParams) (gen.Executor, error) {
	row := q.db.QueryRowContext(ctx,
		`UPDATE executors SET status = 'dead', owner_instance_id = NULL, lease_expires_at = NULL
		 WHERE application_id = ? AND executor_id = ? AND status != 'dead'
		 RETURNING id, application_id, executor_id, application_version, hostname, metadata,
		           status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at`,
		uuidToText(arg.ApplicationID), arg.ExecutorID,
	)
	return scanExecutor(row)
}

func (q *sqliteQueries) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	_, err := q.db.ExecContext(ctx,
		`DELETE FROM executors WHERE application_id = ? AND executor_id = ?`,
		uuidToText(arg.ApplicationID), arg.ExecutorID,
	)
	return mapDBErr(err)
}

func (q *sqliteQueries) ListDeadExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, application_id, executor_id, application_version, hostname, metadata,
		        status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at
		 FROM executors WHERE application_id = ? AND status = 'dead' ORDER BY disconnected_at ASC`,
		uuidToText(applicationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Executor
	for rows.Next() {
		e, err := scanExecutorRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) AdoptExpiredExecutors(ctx context.Context, arg gen.AdoptExpiredExecutorsParams) ([]gen.Executor, error) {
	ownerID := uuidToText(arg.OwnerInstanceID)
	leaseStr := timestamptzToText(arg.LeaseExpiresAt)
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)

	rows, err := q.db.QueryContext(ctx,
		`UPDATE executors SET owner_instance_id = ?, lease_expires_at = ?
		 WHERE status = 'connected'
		   AND (owner_instance_id IS NULL OR owner_instance_id != ?)
		   AND (lease_expires_at IS NULL OR lease_expires_at < ?)
		 RETURNING id, application_id, executor_id, application_version, hostname, metadata,
		           status, owner_instance_id, lease_expires_at, connected_at, last_seen_at, disconnected_at`,
		ownerID, leaseStr, ownerID, nowStr,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Executor
	for rows.Next() {
		e, err := scanExecutorRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) ListApplicationVersionsDistinct(ctx context.Context, applicationID pgtype.UUID) ([]gen.ListApplicationVersionsDistinctRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT application_version, max(connected_at) AS latest_connected_at
		 FROM executors WHERE application_id = ? AND application_version != ''
		 GROUP BY application_version ORDER BY latest_connected_at DESC`,
		uuidToText(applicationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.ListApplicationVersionsDistinctRow
	for rows.Next() {
		var r gen.ListApplicationVersionsDistinctRow
		var timeStr sql.NullString
		if err := rows.Scan(&r.ApplicationVersion, &timeStr); err != nil {
			return nil, err
		}
		r.LatestConnectedAt = textToTimestamptz(timeStr)
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetExecutorCountsGrouped(ctx context.Context) ([]gen.GetExecutorCountsGroupedRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT a.organisation_id, a.name AS application_name, e.application_version, e.status, count(*) AS count
		 FROM executors e
		 JOIN applications a ON e.application_id = a.id
		 GROUP BY a.organisation_id, a.name, e.application_version, e.status`,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.GetExecutorCountsGroupedRow
	for rows.Next() {
		var r gen.GetExecutorCountsGroupedRow
		var orgStr, statusStr string
		if err := rows.Scan(&orgStr, &r.ApplicationName, &r.ApplicationVersion, &statusStr, &r.Count); err != nil {
			return nil, err
		}
		r.OrganisationID = textToUUID(orgStr)
		r.Status = gen.ExecutorStatus(statusStr)
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetExecutorCountsGroupedByOrg(ctx context.Context, organisationID pgtype.UUID) ([]gen.GetExecutorCountsGroupedByOrgRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT a.organisation_id, a.name AS application_name, e.application_version, e.status, count(*) AS count
		 FROM executors e
		 JOIN applications a ON e.application_id = a.id
		 WHERE a.organisation_id = ?
		 GROUP BY a.organisation_id, a.name, e.application_version, e.status`,
		uuidToText(organisationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.GetExecutorCountsGroupedByOrgRow
	for rows.Next() {
		var r gen.GetExecutorCountsGroupedByOrgRow
		var orgStr, statusStr string
		if err := rows.Scan(&orgStr, &r.ApplicationName, &r.ApplicationVersion, &statusStr, &r.Count); err != nil {
			return nil, err
		}
		r.OrganisationID = textToUUID(orgStr)
		r.Status = gen.ExecutorStatus(statusStr)
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func scanExecutor(row *sql.Row) (gen.Executor, error) {
	var e gen.Executor
	var idStr, appStr, statusStr string
	var metaStr string
	var ownerStr, leaseStr, connStr, lastStr, discStr sql.NullString
	if err := row.Scan(&idStr, &appStr, &e.ExecutorID, &e.ApplicationVersion, &e.Hostname, &metaStr,
		&statusStr, &ownerStr, &leaseStr, &connStr, &lastStr, &discStr); err != nil {
		return e, mapDBErr(err)
	}
	e.ID = textToUUID(idStr)
	e.ApplicationID = textToUUID(appStr)
	e.Metadata = []byte(metaStr)
	e.Status = gen.ExecutorStatus(statusStr)
	if ownerStr.Valid {
		e.OwnerInstanceID = textToUUID(ownerStr.String)
	}
	e.LeaseExpiresAt = textToTimestamptz(leaseStr)
	e.ConnectedAt = textToTimestamptz(connStr)
	e.LastSeenAt = textToTimestamptz(lastStr)
	e.DisconnectedAt = textToTimestamptz(discStr)
	return e, nil
}

func scanExecutorRows(rows *sql.Rows) (gen.Executor, error) {
	var e gen.Executor
	var idStr, appStr, statusStr string
	var metaStr string
	var ownerStr, leaseStr, connStr, lastStr, discStr sql.NullString
	if err := rows.Scan(&idStr, &appStr, &e.ExecutorID, &e.ApplicationVersion, &e.Hostname, &metaStr,
		&statusStr, &ownerStr, &leaseStr, &connStr, &lastStr, &discStr); err != nil {
		return e, mapDBErr(err)
	}
	e.ID = textToUUID(idStr)
	e.ApplicationID = textToUUID(appStr)
	e.Metadata = []byte(metaStr)
	e.Status = gen.ExecutorStatus(statusStr)
	if ownerStr.Valid {
		e.OwnerInstanceID = textToUUID(ownerStr.String)
	}
	e.LeaseExpiresAt = textToTimestamptz(leaseStr)
	e.ConnectedAt = textToTimestamptz(connStr)
	e.LastSeenAt = textToTimestamptz(lastStr)
	e.DisconnectedAt = textToTimestamptz(discStr)
	return e, nil
}

// ----------------------------------------------------------------------
// API Keys
// ----------------------------------------------------------------------

func (q *sqliteQueries) CreateAPIKey(ctx context.Context, arg gen.CreateAPIKeyParams) (gen.ApiKey, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	appNames := stringsToJSON(arg.ApplicationNames)
	perms := stringsToJSON(arg.Permissions)

	row := q.db.QueryRowContext(ctx,
		`INSERT INTO api_keys (id, organisation_id, name, lookup, key_hash, application_names, permissions)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 RETURNING id, organisation_id, name, lookup, key_hash, application_names, permissions,
		           created_at, last_used_at, revoked_at`,
		id, uuidToText(arg.OrganisationID), arg.Name, arg.Lookup, arg.KeyHash, appNames, perms,
	)
	return scanAPIKey(row)
}

func (q *sqliteQueries) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, organisation_id, name, lookup, key_hash, application_names, permissions,
		        created_at, last_used_at, revoked_at
		 FROM api_keys WHERE lookup = ? AND revoked_at IS NULL`,
		lookup,
	)
	return scanAPIKey(row)
}

func (q *sqliteQueries) ListAPIKeys(ctx context.Context, organisationID pgtype.UUID) ([]gen.ApiKey, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, name, lookup, key_hash, application_names, permissions,
		        created_at, last_used_at, revoked_at
		 FROM api_keys WHERE organisation_id = ? ORDER BY created_at DESC`,
		uuidToText(organisationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.ApiKey
	for rows.Next() {
		k, err := scanAPIKeyRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, k)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) RevokeAPIKey(ctx context.Context, arg gen.RevokeAPIKeyParams) (gen.ApiKey, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	row := q.db.QueryRowContext(ctx,
		`UPDATE api_keys SET revoked_at = ?
		 WHERE id = ? AND organisation_id = ?
		 RETURNING id, organisation_id, name, lookup, key_hash, application_names, permissions,
		           created_at, last_used_at, revoked_at`,
		nowStr, uuidToText(arg.ID), uuidToText(arg.OrganisationID),
	)
	return scanAPIKey(row)
}

func (q *sqliteQueries) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := q.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		nowStr, uuidToText(id),
	)
	return mapDBErr(err)
}

func scanAPIKey(row *sql.Row) (gen.ApiKey, error) {
	var k gen.ApiKey
	var idStr, orgStr, appJSON, permJSON string
	var createdStr, lastStr, revStr sql.NullString
	if err := row.Scan(&idStr, &orgStr, &k.Name, &k.Lookup, &k.KeyHash, &appJSON, &permJSON,
		&createdStr, &lastStr, &revStr); err != nil {
		return k, mapDBErr(err)
	}
	k.ID = textToUUID(idStr)
	k.OrganisationID = textToUUID(orgStr)
	k.ApplicationNames = jsonToStrings(appJSON)
	k.Permissions = jsonToStrings(permJSON)
	k.CreatedAt = textToTimestamptz(createdStr)
	k.LastUsedAt = textToTimestamptz(lastStr)
	k.RevokedAt = textToTimestamptz(revStr)
	return k, nil
}

func scanAPIKeyRows(rows *sql.Rows) (gen.ApiKey, error) {
	var k gen.ApiKey
	var idStr, orgStr, appJSON, permJSON string
	var createdStr, lastStr, revStr sql.NullString
	if err := rows.Scan(&idStr, &orgStr, &k.Name, &k.Lookup, &k.KeyHash, &appJSON, &permJSON,
		&createdStr, &lastStr, &revStr); err != nil {
		return k, mapDBErr(err)
	}
	k.ID = textToUUID(idStr)
	k.OrganisationID = textToUUID(orgStr)
	k.ApplicationNames = jsonToStrings(appJSON)
	k.Permissions = jsonToStrings(permJSON)
	k.CreatedAt = textToTimestamptz(createdStr)
	k.LastUsedAt = textToTimestamptz(lastStr)
	k.RevokedAt = textToTimestamptz(revStr)
	return k, nil
}

// ----------------------------------------------------------------------
// Alerting Rules
// ----------------------------------------------------------------------

func (q *sqliteQueries) CreateAlertingRule(ctx context.Context, arg gen.CreateAlertingRuleParams) (gen.AlertingRule, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	meta := string(arg.RuleMetadata)
	if meta == "" {
		meta = "{}"
	}
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO alerting_rules (
		    id, application_id, receiving_application_id, rule_type, rule_metadata, min_interval_secs
		 ) VALUES (?, ?, ?, ?, ?, ?)
		 RETURNING id, application_id, receiving_application_id, rule_type, rule_metadata, min_interval_secs, last_fired_at, created_at`,
		id, uuidToText(arg.ApplicationID), uuidToText(arg.ReceivingApplicationID), arg.RuleType, meta, arg.MinIntervalSecs,
	)
	return scanAlertingRule(row)
}

func (q *sqliteQueries) GetAlertingRule(ctx context.Context, arg gen.GetAlertingRuleParams) (gen.AlertingRule, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, application_id, receiving_application_id, rule_type, rule_metadata, min_interval_secs, last_fired_at, created_at
		 FROM alerting_rules WHERE id = ? AND application_id = ?`,
		uuidToText(arg.ID), uuidToText(arg.ApplicationID),
	)
	return scanAlertingRule(row)
}

func (q *sqliteQueries) ListAlertingRulesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.AlertingRule, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, application_id, receiving_application_id, rule_type, rule_metadata, min_interval_secs, last_fired_at, created_at
		 FROM alerting_rules WHERE application_id = ? ORDER BY created_at ASC`,
		uuidToText(applicationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.AlertingRule
	for rows.Next() {
		r, err := scanAlertingRuleRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) DeleteAlertingRule(ctx context.Context, arg gen.DeleteAlertingRuleParams) (int64, error) {
	res, err := q.db.ExecContext(ctx,
		`DELETE FROM alerting_rules WHERE id = ? AND application_id = ?`,
		uuidToText(arg.ID), uuidToText(arg.ApplicationID),
	)
	if err != nil {
		return 0, mapDBErr(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (q *sqliteQueries) TouchAlertRuleLastFired(ctx context.Context, id pgtype.UUID) error {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := q.db.ExecContext(ctx,
		`UPDATE alerting_rules SET last_fired_at = ? WHERE id = ?`,
		nowStr, uuidToText(id),
	)
	return mapDBErr(err)
}

func (q *sqliteQueries) TouchAlertRuleLastFiredAtomic(ctx context.Context, arg gen.TouchAlertRuleLastFiredAtomicParams) (gen.AlertingRule, error) {
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	row := q.db.QueryRowContext(ctx,
		`UPDATE alerting_rules SET last_fired_at = ?
		 WHERE id = ? AND application_id = ?
		   AND (
		     last_fired_at IS NULL
		     OR min_interval_secs IS NULL
		     OR min_interval_secs <= 0
		     OR datetime(last_fired_at, '+' || min_interval_secs || ' seconds') <= ?
		   )
		 RETURNING id, application_id, receiving_application_id, rule_type, rule_metadata, min_interval_secs, last_fired_at, created_at`,
		nowStr, uuidToText(arg.ID), uuidToText(arg.ApplicationID), nowStr,
	)
	return scanAlertingRule(row)
}

func scanAlertingRule(row *sql.Row) (gen.AlertingRule, error) {
	var r gen.AlertingRule
	var idStr, appStr, recvStr string
	var metaStr string
	var minInterval sql.NullInt32
	var lastStr, createdStr sql.NullString
	if err := row.Scan(&idStr, &appStr, &recvStr, &r.RuleType, &metaStr, &minInterval, &lastStr, &createdStr); err != nil {
		return r, mapDBErr(err)
	}
	r.ID = textToUUID(idStr)
	r.ApplicationID = textToUUID(appStr)
	r.ReceivingApplicationID = textToUUID(recvStr)
	r.RuleMetadata = []byte(metaStr)
	if minInterval.Valid {
		v := minInterval.Int32
		r.MinIntervalSecs = &v
	}
	r.LastFiredAt = textToTimestamptz(lastStr)
	r.CreatedAt = textToTimestamptz(createdStr)
	return r, nil
}

func scanAlertingRuleRows(rows *sql.Rows) (gen.AlertingRule, error) {
	var r gen.AlertingRule
	var idStr, appStr, recvStr string
	var metaStr string
	var minInterval sql.NullInt32
	var lastStr, createdStr sql.NullString
	if err := rows.Scan(&idStr, &appStr, &recvStr, &r.RuleType, &metaStr, &minInterval, &lastStr, &createdStr); err != nil {
		return r, mapDBErr(err)
	}
	r.ID = textToUUID(idStr)
	r.ApplicationID = textToUUID(appStr)
	r.ReceivingApplicationID = textToUUID(recvStr)
	r.RuleMetadata = []byte(metaStr)
	if minInterval.Valid {
		v := minInterval.Int32
		r.MinIntervalSecs = &v
	}
	r.LastFiredAt = textToTimestamptz(lastStr)
	r.CreatedAt = textToTimestamptz(createdStr)
	return r, nil
}

// ----------------------------------------------------------------------
// Identity: Users, Roles, Members, Claims, Audits
// ----------------------------------------------------------------------

func (q *sqliteQueries) CreateUser(ctx context.Context, arg gen.CreateUserParams) (gen.User, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	isAdmin := 0
	if arg.IsAdmin {
		isAdmin = 1
	}
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO users (id, subject, username, email, is_admin)
		 VALUES (?, ?, ?, ?, ?)
		 RETURNING id, subject, username, email, is_admin, created_at`,
		id, arg.Subject, arg.Username, arg.Email, isAdmin,
	)
	return scanUser(row)
}

func (q *sqliteQueries) UpsertUser(ctx context.Context, arg gen.UpsertUserParams) (gen.User, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	isAdmin := 0
	if arg.IsAdmin {
		isAdmin = 1
	}
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO users (id, subject, username, email, is_admin)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (subject) DO UPDATE SET
		     username = EXCLUDED.username,
		     email = EXCLUDED.email
		 RETURNING id, subject, username, email, is_admin, created_at`,
		id, arg.Subject, arg.Username, arg.Email, isAdmin,
	)
	return scanUser(row)
}

func (q *sqliteQueries) GetUserBySubject(ctx context.Context, subject string) (gen.User, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, subject, username, email, is_admin, created_at FROM users WHERE subject = ?`,
		subject,
	)
	return scanUser(row)
}

func (q *sqliteQueries) GetUserByUsername(ctx context.Context, username string) (gen.User, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, subject, username, email, is_admin, created_at FROM users WHERE username = ?`,
		username,
	)
	return scanUser(row)
}

func (q *sqliteQueries) GetUserByID(ctx context.Context, id pgtype.UUID) (gen.User, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, subject, username, email, is_admin, created_at FROM users WHERE id = ?`,
		uuidToText(id),
	)
	return scanUser(row)
}

func scanUser(row *sql.Row) (gen.User, error) {
	var u gen.User
	var idStr, createdStr string
	var isAdmin int
	if err := row.Scan(&idStr, &u.Subject, &u.Username, &u.Email, &isAdmin, &createdStr); err != nil {
		return u, mapDBErr(err)
	}
	u.ID = textToUUID(idStr)
	u.IsAdmin = (isAdmin == 1)
	u.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return u, nil
}

func (q *sqliteQueries) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]gen.ListMembersByOrganisationRow, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT u.id AS user_id, u.username, u.email, om.role_name, om.created_at
		 FROM organisation_members om
		 JOIN users u ON om.user_id = u.id
		 WHERE om.organisation_id = ?
		 ORDER BY om.created_at ASC`,
		uuidToText(organisationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.ListMembersByOrganisationRow
	for rows.Next() {
		var r gen.ListMembersByOrganisationRow
		var uidStr, createdStr string
		if err := rows.Scan(&uidStr, &r.Username, &r.Email, &r.RoleName, &createdStr); err != nil {
			return nil, err
		}
		r.UserID = textToUUID(uidStr)
		r.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetMember(ctx context.Context, arg gen.GetMemberParams) (gen.GetMemberRow, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT u.id AS user_id, u.username, u.email, om.role_name, om.created_at
		 FROM organisation_members om
		 JOIN users u ON om.user_id = u.id
		 WHERE om.organisation_id = ? AND u.username = ?`,
		uuidToText(arg.OrganisationID), arg.Username,
	)
	var r gen.GetMemberRow
	var uidStr, createdStr string
	if err := row.Scan(&uidStr, &r.Username, &r.Email, &r.RoleName, &createdStr); err != nil {
		return r, mapDBErr(err)
	}
	r.UserID = textToUUID(uidStr)
	r.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return r, nil
}

func (q *sqliteQueries) UpsertMemberRole(ctx context.Context, arg gen.UpsertMemberRoleParams) (gen.OrganisationMember, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO organisation_members (id, organisation_id, user_id, role_name)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (organisation_id, user_id) DO UPDATE SET role_name = EXCLUDED.role_name
		 RETURNING id, organisation_id, user_id, role_name, created_at`,
		id, uuidToText(arg.OrganisationID), uuidToText(arg.UserID), arg.RoleName,
	)
	var om gen.OrganisationMember
	var idStr, orgStr, userStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &userStr, &om.RoleName, &createdStr); err != nil {
		return om, mapDBErr(err)
	}
	om.ID = textToUUID(idStr)
	om.OrganisationID = textToUUID(orgStr)
	om.UserID = textToUUID(userStr)
	om.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return om, nil
}

func (q *sqliteQueries) RemoveMember(ctx context.Context, arg gen.RemoveMemberParams) (gen.OrganisationMember, error) {
	row := q.db.QueryRowContext(ctx,
		`DELETE FROM organisation_members WHERE organisation_id = ? AND user_id = ?
		 RETURNING id, organisation_id, user_id, role_name, created_at`,
		uuidToText(arg.OrganisationID), uuidToText(arg.UserID),
	)
	var om gen.OrganisationMember
	var idStr, orgStr, userStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &userStr, &om.RoleName, &createdStr); err != nil {
		return om, mapDBErr(err)
	}
	om.ID = textToUUID(idStr)
	om.OrganisationID = textToUUID(orgStr)
	om.UserID = textToUUID(userStr)
	om.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return om, nil
}

func (q *sqliteQueries) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (gen.GetUserPrimaryOrganisationRow, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT o.id, o.name, o.created_at, om.role_name
		 FROM organisations o
		 JOIN organisation_members om ON o.id = om.organisation_id
		 WHERE om.user_id = ?
		 ORDER BY om.created_at ASC
		 LIMIT 1`,
		uuidToText(userID),
	)
	var r gen.GetUserPrimaryOrganisationRow
	var idStr, createdStr string
	if err := row.Scan(&idStr, &r.Name, &createdStr, &r.RoleName); err != nil {
		return r, mapDBErr(err)
	}
	r.ID = textToUUID(idStr)
	r.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return r, nil
}

func (q *sqliteQueries) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]gen.Role, error) {
	orgID := uuidToText(organisationID)
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, name, permissions, is_global, created_at
		 FROM roles WHERE organisation_id = ? OR organisation_id IS NULL
		 ORDER BY is_global DESC, name ASC`,
		orgID,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.Role
	for rows.Next() {
		r, err := scanRoleRows(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetRole(ctx context.Context, arg gen.GetRoleParams) (gen.Role, error) {
	orgID := uuidToText(arg.OrganisationID)
	row := q.db.QueryRowContext(ctx,
		`SELECT id, organisation_id, name, permissions, is_global, created_at
		 FROM roles WHERE (organisation_id = ? OR organisation_id IS NULL) AND name = ?
		 LIMIT 1`,
		orgID, arg.Name,
	)
	return scanRole(row)
}

func (q *sqliteQueries) CreateRole(ctx context.Context, arg gen.CreateRoleParams) (gen.Role, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	perms := stringsToJSON(arg.Permissions)
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO roles (id, organisation_id, name, permissions, is_global)
		 VALUES (?, ?, ?, ?, 0)
		 RETURNING id, organisation_id, name, permissions, is_global, created_at`,
		id, uuidToText(arg.OrganisationID), arg.Name, perms,
	)
	return scanRole(row)
}

func (q *sqliteQueries) DeleteRole(ctx context.Context, arg gen.DeleteRoleParams) (gen.Role, error) {
	row := q.db.QueryRowContext(ctx,
		`DELETE FROM roles WHERE organisation_id = ? AND name = ? AND is_global = 0
		 RETURNING id, organisation_id, name, permissions, is_global, created_at`,
		uuidToText(arg.OrganisationID), arg.Name,
	)
	return scanRole(row)
}

func scanRole(row *sql.Row) (gen.Role, error) {
	var r gen.Role
	var idStr, permJSON, createdStr string
	var orgStr sql.NullString
	var isGlobal int
	if err := row.Scan(&idStr, &orgStr, &r.Name, &permJSON, &isGlobal, &createdStr); err != nil {
		return r, mapDBErr(err)
	}
	r.ID = textToUUID(idStr)
	if orgStr.Valid {
		r.OrganisationID = textToUUID(orgStr.String)
	}
	r.Permissions = jsonToStrings(permJSON)
	r.IsGlobal = (isGlobal == 1)
	r.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return r, nil
}

func scanRoleRows(rows *sql.Rows) (gen.Role, error) {
	var r gen.Role
	var idStr, permJSON, createdStr string
	var orgStr sql.NullString
	var isGlobal int
	if err := rows.Scan(&idStr, &orgStr, &r.Name, &permJSON, &isGlobal, &createdStr); err != nil {
		return r, mapDBErr(err)
	}
	r.ID = textToUUID(idStr)
	if orgStr.Valid {
		r.OrganisationID = textToUUID(orgStr.String)
	}
	r.Permissions = jsonToStrings(permJSON)
	r.IsGlobal = (isGlobal == 1)
	r.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return r, nil
}

func (q *sqliteQueries) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]gen.DomainClaim, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, domain, created_at
		 FROM domain_claims WHERE organisation_id = ? ORDER BY domain ASC`,
		uuidToText(organisationID),
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.DomainClaim
	for rows.Next() {
		var dc gen.DomainClaim
		var idStr, orgStr, createdStr string
		if err := rows.Scan(&idStr, &orgStr, &dc.Domain, &createdStr); err != nil {
			return nil, err
		}
		dc.ID = textToUUID(idStr)
		dc.OrganisationID = textToUUID(orgStr)
		dc.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
		list = append(list, dc)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) GetDomainClaim(ctx context.Context, domain string) (gen.DomainClaim, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id, organisation_id, domain, created_at FROM domain_claims WHERE domain = ?`,
		domain,
	)
	var dc gen.DomainClaim
	var idStr, orgStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &dc.Domain, &createdStr); err != nil {
		return dc, mapDBErr(err)
	}
	dc.ID = textToUUID(idStr)
	dc.OrganisationID = textToUUID(orgStr)
	dc.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return dc, nil
}

func (q *sqliteQueries) CreateDomainClaim(ctx context.Context, arg gen.CreateDomainClaimParams) (gen.DomainClaim, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	row := q.db.QueryRowContext(ctx,
		`INSERT INTO domain_claims (id, organisation_id, domain) VALUES (?, ?, ?)
		 RETURNING id, organisation_id, domain, created_at`,
		id, uuidToText(arg.OrganisationID), arg.Domain,
	)
	var dc gen.DomainClaim
	var idStr, orgStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &dc.Domain, &createdStr); err != nil {
		return dc, mapDBErr(err)
	}
	dc.ID = textToUUID(idStr)
	dc.OrganisationID = textToUUID(orgStr)
	dc.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return dc, nil
}

func (q *sqliteQueries) DeleteDomainClaim(ctx context.Context, arg gen.DeleteDomainClaimParams) (gen.DomainClaim, error) {
	row := q.db.QueryRowContext(ctx,
		`DELETE FROM domain_claims WHERE organisation_id = ? AND domain = ?
		 RETURNING id, organisation_id, domain, created_at`,
		uuidToText(arg.OrganisationID), arg.Domain,
	)
	var dc gen.DomainClaim
	var idStr, orgStr, createdStr string
	if err := row.Scan(&idStr, &orgStr, &dc.Domain, &createdStr); err != nil {
		return dc, mapDBErr(err)
	}
	dc.ID = textToUUID(idStr)
	dc.OrganisationID = textToUUID(orgStr)
	dc.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return dc, nil
}

func (q *sqliteQueries) CreateAuditLog(ctx context.Context, arg gen.CreateAuditLogParams) (gen.AuditLog, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	var userID *string
	if arg.UserID.Valid {
		s := uuidToText(arg.UserID)
		userID = &s
	}
	details := string(arg.Details)
	if details == "" {
		details = "{}"
	}

	row := q.db.QueryRowContext(ctx,
		`INSERT INTO audit_logs (id, organisation_id, user_id, username, action, details)
		 VALUES (?, ?, ?, ?, ?, ?)
		 RETURNING id, organisation_id, user_id, username, action, details, created_at`,
		id, uuidToText(arg.OrganisationID), userID, arg.Username, arg.Action, details,
	)
	var al gen.AuditLog
	var idStr, orgStr, detStr, createdStr string
	var uStr sql.NullString
	if err := row.Scan(&idStr, &orgStr, &uStr, &al.Username, &al.Action, &detStr, &createdStr); err != nil {
		return al, mapDBErr(err)
	}
	al.ID = textToUUID(idStr)
	al.OrganisationID = textToUUID(orgStr)
	if uStr.Valid {
		al.UserID = textToUUID(uStr.String)
	}
	al.Details = []byte(detStr)
	al.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
	return al, nil
}

func (q *sqliteQueries) ListAuditLogs(ctx context.Context, arg gen.ListAuditLogsParams) ([]gen.AuditLog, error) {
	startStr := timestamptzToText(arg.StartTime)
	endStr := timestamptzToText(arg.EndTime)

	rows, err := q.db.QueryContext(ctx,
		`SELECT id, organisation_id, user_id, username, action, details, created_at
		 FROM audit_logs
		 WHERE organisation_id = ?
		   AND (? IS NULL OR created_at >= ?)
		   AND (? IS NULL OR created_at <= ?)
		   AND (? IS NULL OR action = ?)
		   AND (? IS NULL OR username = ?
		       OR json_extract(details, '$.subject_display') = ?
		       OR json_extract(details, '$.subject_id') = ?)
		   AND (? IS NULL OR json_extract(details, '$.target_id') = ?)
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		uuidToText(arg.OrganisationID),
		startStr, startStr,
		endStr, endStr,
		arg.Operation, arg.Operation,
		arg.Subject, arg.Subject, arg.Subject, arg.Subject,
		arg.Target, arg.Target,
		arg.Limit, arg.Offset,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.AuditLog
	for rows.Next() {
		var al gen.AuditLog
		var idStr, orgStr, detStr, createdStr string
		var uStr sql.NullString
		if err := rows.Scan(&idStr, &orgStr, &uStr, &al.Username, &al.Action, &detStr, &createdStr); err != nil {
			return nil, err
		}
		al.ID = textToUUID(idStr)
		al.OrganisationID = textToUUID(orgStr)
		if uStr.Valid {
			al.UserID = textToUUID(uStr.String)
		}
		al.Details = []byte(detStr)
		al.CreatedAt = textToTimestamptz(sql.NullString{String: createdStr, Valid: true})
		list = append(list, al)
	}
	return list, mapDBErr(rows.Err())
}

func (q *sqliteQueries) DeleteExpiredAuditLogs(ctx context.Context, arg gen.DeleteExpiredAuditLogsParams) (int64, error) {
	cutoff := timestamptzToText(arg.Cutoff)
	res, err := q.db.ExecContext(ctx,
		`DELETE FROM audit_logs WHERE organisation_id = ? AND created_at < ?`,
		uuidToText(arg.OrganisationID), cutoff,
	)
	if err != nil {
		return 0, mapDBErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, mapDBErr(err)
	}
	return n, nil
}

// ----------------------------------------------------------------------
// Recovery Dispatches
// ----------------------------------------------------------------------

func (q *sqliteQueries) RecordRecoveryDispatch(ctx context.Context, arg gen.RecordRecoveryDispatchParams) (gen.RecoveryDispatch, error) {
	id := uuidToText(pgtype.UUID{Valid: false})
	dispStr := timestamptzToText(arg.DispatchedAt)
	if dispStr == nil {
		s := time.Now().UTC().Format(time.RFC3339Nano)
		dispStr = &s
	}
	successInt := 0
	if arg.Success {
		successInt = 1
	}

	row := q.db.QueryRowContext(ctx,
		`INSERT INTO recovery_dispatches (id, application_id, dead_executor_id, target_executor_id, dispatched_at, success)
		 VALUES (?, ?, ?, ?, ?, ?)
		 RETURNING id, application_id, dead_executor_id, target_executor_id, dispatched_at, success`,
		id, uuidToText(arg.ApplicationID), arg.DeadExecutorID, arg.TargetExecutorID, dispStr, successInt,
	)
	var rd gen.RecoveryDispatch
	var idStr, appStr, dtStr string
	var scInt int
	if err := row.Scan(&idStr, &appStr, &rd.DeadExecutorID, &rd.TargetExecutorID, &dtStr, &scInt); err != nil {
		return rd, mapDBErr(err)
	}
	rd.ID = textToUUID(idStr)
	rd.ApplicationID = textToUUID(appStr)
	rd.DispatchedAt = textToTimestamptz(sql.NullString{String: dtStr, Valid: true})
	rd.Success = (scInt == 1)
	return rd, nil
}

func (q *sqliteQueries) CountRecentRecoveryDispatches(ctx context.Context, arg gen.CountRecentRecoveryDispatchesParams) (int64, error) {
	dtStr := timestamptzToText(arg.DispatchedAt)
	row := q.db.QueryRowContext(ctx,
		`SELECT count(*) FROM recovery_dispatches
		 WHERE application_id = ? AND dead_executor_id = ? AND dispatched_at >= ?`,
		uuidToText(arg.ApplicationID), arg.DeadExecutorID, dtStr,
	)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, mapDBErr(err)
	}
	return count, nil
}

func (q *sqliteQueries) ListRecentRecoveryDispatches(ctx context.Context, arg gen.ListRecentRecoveryDispatchesParams) ([]gen.RecoveryDispatch, error) {
	dtStr := timestamptzToText(arg.DispatchedAt)
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, application_id, dead_executor_id, target_executor_id, dispatched_at, success
		 FROM recovery_dispatches
		 WHERE application_id = ? AND dispatched_at >= ?
		 ORDER BY dispatched_at DESC LIMIT ?`,
		uuidToText(arg.ApplicationID), dtStr, arg.Limit,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer func() { _ = rows.Close() }()

	var list []gen.RecoveryDispatch
	for rows.Next() {
		var rd gen.RecoveryDispatch
		var idStr, appStr, retDtStr string
		var scInt int
		if err := rows.Scan(&idStr, &appStr, &rd.DeadExecutorID, &rd.TargetExecutorID, &retDtStr, &scInt); err != nil {
			return nil, err
		}
		rd.ID = textToUUID(idStr)
		rd.ApplicationID = textToUUID(appStr)
		rd.DispatchedAt = textToTimestamptz(sql.NullString{String: retDtStr, Valid: true})
		rd.Success = (scInt == 1)
		list = append(list, rd)
	}
	return list, mapDBErr(rows.Err())
}
