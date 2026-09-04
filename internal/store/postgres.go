package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"check-flight/internal/model"
)

type PostgresRepository struct {
	db *sql.DB
}

var _ Storage = (*PostgresRepository)(nil)

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Init() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS flights (
			uid TEXT PRIMARY KEY,
			internal_id TEXT,
			provider TEXT NOT NULL,
			direction TEXT NOT NULL,
			flight_code TEXT NOT NULL,
			city TEXT,
			sched_time TEXT NOT NULL,
			estimated_time TEXT,
			status TEXT,
			gate TEXT,
			terminal TEXT,
			baggage_belt TEXT,
			check_in_desk TEXT,
			gate_changed BOOLEAN NOT NULL DEFAULT FALSE,
			baggage_belt_changed BOOLEAN NOT NULL DEFAULT FALSE,
			check_in_desk_changed BOOLEAN NOT NULL DEFAULT FALSE,
			updated_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_flights_search ON flights (flight_code, sched_time)`,
		`CREATE INDEX IF NOT EXISTS idx_flights_provider_dir ON flights (provider, direction)`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id BIGSERIAL PRIMARY KEY,
			chat_id BIGINT NOT NULL,
			flight_code TEXT NOT NULL,
			active_uid TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(chat_id, flight_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subs_uid ON subscriptions(active_uid)`,
		`ALTER TABLE flights ADD COLUMN IF NOT EXISTS estimated_time TEXT`,
		`ALTER TABLE flights ADD COLUMN IF NOT EXISTS gate_changed BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE flights ADD COLUMN IF NOT EXISTS baggage_belt_changed BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE flights ADD COLUMN IF NOT EXISTS check_in_desk_changed BOOLEAN NOT NULL DEFAULT FALSE`,
	}
	for _, query := range queries {
		if _, err := r.db.Exec(query); err != nil {
			return fmt.Errorf("postgres migration failed: %w", err)
		}
	}
	return nil
}

const postgresFlightColumns = `uid, internal_id, provider, direction, flight_code, city, sched_time, estimated_time, status, gate, terminal, baggage_belt, check_in_desk, gate_changed, baggage_belt_changed, check_in_desk_changed`

type postgresScanner interface {
	Scan(dest ...any) error
}

func scanPostgresFlight(scanner postgresScanner, f *model.Flight) error {
	return scanner.Scan(
		&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City,
		&f.SchedTime, &f.EstimatedTime, &f.Status, &f.Gate, &f.Terminal,
		&f.BaggageBelt, &f.CheckInDesk, &f.GateChanged, &f.BaggageBeltChanged,
		&f.CheckInDeskChanged,
	)
}

func (r *PostgresRepository) GetOldSubscriptions(ctx context.Context) ([]OldSub, error) {
	threshold := time.Now().In(time.FixedZone("MSK", 3*60*60)).Add(-30 * 24 * time.Hour)
	rows, err := r.db.QueryContext(ctx, `SELECT chat_id, flight_code, active_uid, created_at FROM subscriptions WHERE created_at < $1`, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []OldSub
	for rows.Next() {
		var sub OldSub
		if err := rows.Scan(&sub.ChatID, &sub.FlightCode, &sub.ActiveUID, &sub.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, sub)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) SwitchSubscriptionUID(ctx context.Context, chatID int64, flightCode, newUID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE subscriptions SET active_uid = $1, created_at = CURRENT_TIMESTAMP WHERE chat_id = $2 AND flight_code = $3`, newUID, chatID, flightCode)
	return err
}

func (r *PostgresRepository) DeleteOldFlights(ctx context.Context) (int64, error) {
	threshold := time.Now().In(time.FixedZone("MSK", 3*60*60)).Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	result, err := r.db.ExecContext(ctx, `DELETE FROM flights WHERE sched_time < $1`, threshold)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return count, err
}

func (r *PostgresRepository) FindUpcomingFlights(ctx context.Context, rawCode string) ([]model.Flight, error) {
	code := NormalizeFlightCode(rawCode)
	threshold := time.Now().In(time.FixedZone("MSK", 3*60*60)).Add(-6 * time.Hour).Format(time.RFC3339)
	return r.findFlights(ctx, `WHERE flight_code = $1 AND sched_time >= $2 ORDER BY sched_time ASC LIMIT 5`, code, threshold)
}

func (r *PostgresRepository) GetFlightByUID(ctx context.Context, uid string) (*model.Flight, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+postgresFlightColumns+` FROM flights WHERE uid = $1`, uid)
	var flight model.Flight
	if err := scanPostgresFlight(row, &flight); err != nil {
		return nil, err
	}
	return &flight, nil
}

func (r *PostgresRepository) GetFlightsByCity(ctx context.Context, city string) ([]model.Flight, error) {
	return r.findFlights(ctx, `WHERE city = $1 ORDER BY sched_time ASC`, city)
}

func (r *PostgresRepository) GetNextFlight(ctx context.Context, code, afterTime string) (*model.Flight, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+postgresFlightColumns+` FROM flights WHERE flight_code = $1 AND sched_time > $2 ORDER BY sched_time ASC LIMIT 1`, code, afterTime)
	var flight model.Flight
	if err := scanPostgresFlight(row, &flight); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &flight, nil
}

func (r *PostgresRepository) findFlights(ctx context.Context, condition string, args ...any) ([]model.Flight, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+postgresFlightColumns+` FROM flights `+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flights []model.Flight
	for rows.Next() {
		var flight model.Flight
		if err := scanPostgresFlight(rows, &flight); err != nil {
			return nil, err
		}
		flights = append(flights, flight)
	}
	return flights, rows.Err()
}

func (r *PostgresRepository) SubscribeToUID(ctx context.Context, chatID int64, code, uid string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO subscriptions (chat_id, flight_code, active_uid) VALUES ($1, $2, $3) ON CONFLICT (chat_id, flight_code) DO UPDATE SET active_uid = EXCLUDED.active_uid, created_at = CURRENT_TIMESTAMP`, chatID, code, uid)
	return err
}

func (r *PostgresRepository) RemoveSubscription(ctx context.Context, chatID int64, flightCode string) error {
	flightCode = NormalizeFlightCode(flightCode)
	_, err := r.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE chat_id = $1 AND flight_code = $2`, chatID, flightCode)
	return err
}

func (r *PostgresRepository) GetSubscribersForUID(ctx context.Context, uid string) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT chat_id FROM subscriptions WHERE active_uid = $1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, err
		}
		result = append(result, chatID)
	}
	return result, rows.Err()
}

func (r *PostgresRepository) GetUserSubscriptionsList(ctx context.Context, chatID int64) ([]model.Flight, error) {
	query := `SELECT ` + postgresFlightColumnsWithAlias("f") + ` FROM subscriptions s JOIN flights f ON s.active_uid = f.uid WHERE s.chat_id = $1`
	rows, err := r.db.QueryContext(ctx, query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flights []model.Flight
	for rows.Next() {
		var flight model.Flight
		if err := scanPostgresFlight(rows, &flight); err != nil {
			return nil, err
		}
		flights = append(flights, flight)
	}
	return flights, rows.Err()
}

func postgresFlightColumnsWithAlias(alias string) string {
	columns := []string{"uid", "internal_id", "provider", "direction", "flight_code", "city", "sched_time", "estimated_time", "status", "gate", "terminal", "baggage_belt", "check_in_desk", "gate_changed", "baggage_belt_changed", "check_in_desk_changed"}
	for i := range columns {
		columns[i] = alias + "." + columns[i]
	}
	return joinSQLColumns(columns)
}

func joinSQLColumns(columns []string) string {
	result := ""
	for i, column := range columns {
		if i > 0 {
			result += ", "
		}
		result += column
	}
	return result
}

func (r *PostgresRepository) LoadFlights(ctx context.Context) (map[string]model.Flight, error) {
	flights, err := r.findFlights(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make(map[string]model.Flight, len(flights))
	for _, flight := range flights {
		result[flight.UID] = flight
	}
	return result, nil
}

func (r *PostgresRepository) SaveChanges(ctx context.Context, updates, inserts []model.Flight) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tx begin error: %w", err)
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	updateQuery := `UPDATE flights SET estimated_time=$1, status=$2, gate=$3, terminal=$4, baggage_belt=$5, check_in_desk=$6, gate_changed=$7, baggage_belt_changed=$8, check_in_desk_changed=$9, updated_at=CURRENT_TIMESTAMP WHERE uid=$10`
	for _, flight := range updates {
		if _, err := tx.ExecContext(ctx, updateQuery, flight.EstimatedTime, flight.Status, flight.Gate, flight.Terminal, flight.BaggageBelt, flight.CheckInDesk, flight.GateChanged, flight.BaggageBeltChanged, flight.CheckInDeskChanged, flight.UID); err != nil {
			return rollback(err)
		}
	}

	insertQuery := `INSERT INTO flights (uid, internal_id, provider, direction, flight_code, city, sched_time, estimated_time, status, gate, terminal, baggage_belt, check_in_desk, gate_changed, baggage_belt_changed, check_in_desk_changed, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, CURRENT_TIMESTAMP) ON CONFLICT (uid) DO UPDATE SET estimated_time=EXCLUDED.estimated_time, status=EXCLUDED.status, gate=EXCLUDED.gate, terminal=EXCLUDED.terminal, baggage_belt=EXCLUDED.baggage_belt, check_in_desk=EXCLUDED.check_in_desk, gate_changed=EXCLUDED.gate_changed, baggage_belt_changed=EXCLUDED.baggage_belt_changed, check_in_desk_changed=EXCLUDED.check_in_desk_changed, updated_at=CURRENT_TIMESTAMP`
	for _, flight := range inserts {
		if _, err := tx.ExecContext(ctx, insertQuery, flight.UID, flight.InternalID, flight.Provider, flight.Direction, flight.Code, flight.City, flight.SchedTime, flight.EstimatedTime, flight.Status, flight.Gate, flight.Terminal, flight.BaggageBelt, flight.CheckInDesk, flight.GateChanged, flight.BaggageBeltChanged, flight.CheckInDeskChanged); err != nil {
			return rollback(err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tx commit error: %w", err)
	}
	return nil
}
