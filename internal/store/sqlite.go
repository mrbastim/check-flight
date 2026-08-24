package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"check-flight/internal/model"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Init(db *sql.DB) error {
	flightsQuery := `
		CREATE TABLE IF NOT EXISTS flights (
		uid          TEXT PRIMARY KEY,  -- "svo:dep:SU1484:2026-08-06T00:05:00+03:00"
		internal_id  TEXT,              -- "1234567"
		provider     TEXT NOT NULL,     -- "svo", "dme", "led"
		direction    TEXT NOT NULL,     -- "dep", "arr"
		flight_code  TEXT NOT NULL,     -- "SU 1484"
		city         TEXT,              -- "Уфа"
		sched_time   DATETIME NOT NULL, -- 2026-08-06T00:05:00+03:00
		status       TEXT,
		gate         TEXT,
		terminal     TEXT,
		baggage_belt TEXT,
		updated_at   DATETIME
		);

		CREATE INDEX IF NOT EXISTS idx_flights_search 
			ON flights (flight_code, sched_time);

		CREATE INDEX IF NOT EXISTS idx_flights_provider_dir 
			ON flights (provider, direction);`
	_, err := db.Exec(flightsQuery)
	if err != nil {
		return err
	}

	subsQuery := `
		CREATE TABLE IF NOT EXISTS subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			flight_code TEXT NOT NULL,
			active_uid TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(chat_id, flight_code)
		);
		CREATE INDEX IF NOT EXISTS idx_subs_uid ON subscriptions(active_uid);
		`
	_, err = db.Exec(subsQuery)
	if err != nil {
		return err
	}

	return nil
}

func NormalizeFlightCode(code string) string {
	clean := strings.ToUpper(strings.TrimSpace(code))
	if len(clean) > 3 && clean[2] != ' ' {
		clean = clean[:2] + " " + clean[2:]
	}
	return clean
}

func (r *Repository) FindUpcomingFlights(ctx context.Context, rawCode string) ([]model.Flight, error) {
	code := NormalizeFlightCode(rawCode)
	msk := time.FixedZone("MSK", 3*60*60)
	threshold := time.Now().In(msk).Add(-6 * time.Hour).Format(time.RFC3339)

	query := `SELECT uid, internal_id, provider, direction, flight_code, city, sched_time, status, gate, terminal 
				FROM flights WHERE flight_code = ? AND sched_time >= ? ORDER BY sched_time ASC LIMIT 5`

	rows, err := r.db.QueryContext(ctx, query, code, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flights []model.Flight
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal); err == nil {
			flights = append(flights, f)
		}
	}
	return flights, nil
}

func (r *Repository) GetFlightByUID(ctx context.Context, uid string) (*model.Flight, error) {
	query := `SELECT uid, internal_id, provider, direction, flight_code, city, sched_time, status, gate, terminal FROM flights WHERE uid = ?`
	row := r.db.QueryRowContext(ctx, query, uid)
	var f model.Flight
	err := row.Scan(&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repository) GetFlightsByCity(ctx context.Context, city string) ([]model.Flight, error) {
	query := `SELECT uid, internal_id, provider, direction, flight_code, city, sched_time, status, gate, terminal 
				FROM flights WHERE city = ? ORDER BY sched_time ASC`
	rows, err := r.db.QueryContext(ctx, query, city)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flights []model.Flight
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal); err == nil {
			flights = append(flights, f)
		}
	}
	return flights, nil
}

func (r *Repository) GetNextFlight(ctx context.Context, code string, afterTime string) (*model.Flight, error) {
	query := `SELECT uid, internal_id, provider, direction, flight_code, city, sched_time, status, gate, terminal 
				FROM flights WHERE flight_code = ? AND sched_time > ? ORDER BY sched_time ASC LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, code, afterTime)
	var f model.Flight
	err := row.Scan(&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate,
		&f.Terminal)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repository) SubscribeToUID(ctx context.Context, chatID int64, code, uid string) error {
	query := `
			INSERT INTO subscriptions (chat_id, flight_code, active_uid) 
			VALUES (?, ?, ?) 
			ON CONFLICT(chat_id, flight_code) 
			DO UPDATE SET active_uid = excluded.active_uid`
	_, err := r.db.ExecContext(ctx, query, chatID, code, uid)
	return err
}

func (r *Repository) RemoveSubscription(ctx context.Context, chatID int64, flightCode string) error {
	flightCode = NormalizeFlightCode(flightCode)
	_, err := r.db.ExecContext(ctx, "DELETE FROM subscriptions WHERE chat_id=? AND flight_code=?", chatID, flightCode)
	return err
}

func (r *Repository) GetSubscriptions(ctx context.Context, chatID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT flight_code FROM subscriptions WHERE chat_id=?", chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return codes, nil
}

func (r *Repository) GetSubscribersForUID(ctx context.Context, uid string) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT chat_id FROM subscriptions WHERE active_uid=?", uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err == nil {
			chatIDs = append(chatIDs, chatID)
		}
	}
	return chatIDs, nil
}

func (r *Repository) UpdateSubscriptionUID(ctx context.Context, oldUID, newUID string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE subscriptions SET active_uid = ? WHERE active_uid = ?", newUID, oldUID)
	return err
}

func (r *Repository) GetUserSubscriptionsList(ctx context.Context, chatID int64) ([]model.Flight, error) {
	query := `
			SELECT f.uid, f.internal_id, f.provider, f.direction, f.flight_code, f.city, f.sched_time, f.status, f.gate, f.terminal 
			FROM subscriptions s
			JOIN flights f ON s.active_uid = f.uid
			WHERE s.chat_id = ?`
	rows, err := r.db.QueryContext(ctx, query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []model.Flight
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.InternalID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal); err == nil {
			res = append(res, f)
		}
	}
	return res, nil
}

func (r *Repository) LoadFlights(ctx context.Context) (map[string]model.Flight, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT uid, provider, internal_id, direction, flight_code, city, sched_time, status, gate, terminal, baggage_belt FROM flights")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]model.Flight)
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.Provider, &f.InternalID, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal, &f.BaggageBelt); err == nil {
			res[f.UID] = f
		}
	}
	return res, nil
}

func (r *Repository) SaveChanges(ctx context.Context, updates []model.Flight, inserts []model.Flight) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tx begin error: %w", err)
	}

	if len(updates) > 0 {
		updateStmt, err := tx.PrepareContext(ctx, `UPDATE flights SET status=?, gate=?, terminal=?, baggage_belt=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer updateStmt.Close()

		for _, f := range updates {
			if _, err := updateStmt.ExecContext(ctx, f.Status, f.Gate, f.Terminal, f.BaggageBelt, f.UID); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	if len(inserts) > 0 {
		insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO flights (uid, internal_id, provider, direction, flight_code, city, sched_time, status, gate, terminal, baggage_belt, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer insertStmt.Close()

		for _, f := range inserts {
			if _, err := insertStmt.ExecContext(ctx, f.UID, f.InternalID, f.Provider, f.Direction, f.Code, f.City, f.SchedTime, f.Status, f.Gate, f.Terminal, f.BaggageBelt); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tx commit error: %w", err)
	}

	return nil
}

func (r *Repository) UpdateFlight(ctx context.Context, tx *sql.Tx, f model.Flight) error {
	updateQuery := `UPDATE flights SET status=?, gate=?, terminal=?, baggage_belt=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`
	_, err := tx.ExecContext(ctx, updateQuery, f.Status, f.Gate, f.Terminal, f.BaggageBelt, f.UID)
	if err != nil {
		return fmt.Errorf("tx update error: %w", err)
	}
	return nil
}

func (r *Repository) InsertFlight(ctx context.Context, tx *sql.Tx, f model.Flight) error {
	insertQuery := `INSERT INTO flights (provider, uid, internal_id, direction, flight_code, city, sched_time, status, gate, terminal, baggage_belt, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := tx.ExecContext(ctx, insertQuery, f.Provider, f.UID, f.InternalID, f.Direction, f.Code, f.City, f.SchedTime, f.Status, f.Gate, f.Terminal, f.BaggageBelt)
	if err != nil {
		return fmt.Errorf("tx insert error: %w", err)
	}
	return nil
}
