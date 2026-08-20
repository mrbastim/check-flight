package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
    provider     TEXT NOT NULL,     -- "svo", "dme", "led"
    direction    TEXT NOT NULL,     -- "dep", "arr"
    flight_code  TEXT NOT NULL,     -- "SU 1484"
    city         TEXT,              -- "Уфа"
    sched_time   DATETIME NOT NULL, -- 2026-08-06T00:05:00+03:00
    status       TEXT,
    gate         TEXT,
    terminal     TEXT,
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
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id INTEGER NOT NULL,
		flight_code TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(chat_id, flight_code)
	);
	CREATE INDEX IF NOT EXISTS idx_subs_flight ON subscriptions(flight_code);
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

func (r *Repository) AddSubscription(ctx context.Context, chatID int64, flightCode string) error {
	flightCode = NormalizeFlightCode(flightCode)
	_, err := r.db.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions (chat_id, flight_code) VALUES (?, ?)", chatID, flightCode)
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

func (r *Repository) GetSubscribersForFlight(ctx context.Context, flightCode string) ([]int64, error) {
	flightCode = NormalizeFlightCode(flightCode)
	rows, err := r.db.QueryContext(ctx, "SELECT chat_id FROM subscriptions WHERE flight_code=?", flightCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, err
		}
		chatIDs = append(chatIDs, chatID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chatIDs, nil
}

func (r *Repository) FindLatestFlight(ctx context.Context, rawCode string) (*model.Flight, error) {
	code := NormalizeFlightCode(rawCode)

	msk := time.FixedZone("MSK", 3*60*60) // Время по Москве (аэропорты МСК)
	// Берем с запасом 3 часа назад, чтобы поймать рейсы, у которых прямо сейчас идет посадка или задержка
	threshold := time.Now().In(msk).Add(-3 * time.Hour).Format(time.RFC3339)

	queryUpcoming := `
		SELECT uid, provider, direction, flight_code, city, sched_time, status, gate, terminal 
		FROM flights 
		WHERE flight_code = ? AND sched_time >= ? 
		ORDER BY sched_time ASC 
		LIMIT 1`

	row := r.db.QueryRowContext(ctx, queryUpcoming, code, threshold)
	var f model.Flight
	err := row.Scan(&f.UID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal)
	if err == nil {
		return &f, nil
	}

	queryFallback := `
		SELECT uid, provider, direction, flight_code, city, sched_time, status, gate, terminal 
		FROM flights 
		WHERE flight_code = ? 
		ORDER BY sched_time DESC 
		LIMIT 1`

	row = r.db.QueryRowContext(ctx, queryFallback, code)
	err = row.Scan(&f.UID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal)
	if err != nil {
		return nil, err
	}

	return &f, nil
}

func (r *Repository) LoadFlights(ctx context.Context) (map[string]model.Flight, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT uid, provider, direction, flight_code, city, sched_time, status, gate, terminal FROM flights")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]model.Flight)
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.Provider, &f.Direction, &f.Code, &f.City, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal); err != nil {
			log.Println("Ошибка при сканировании строки:", err)
			continue
		}
		res[f.UID] = f
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

func (r *Repository) SaveChanges(ctx context.Context, updates []model.Flight, inserts []model.Flight) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("tx begin error: %w", err)
	}

	if len(updates) > 0 {
		updateStmt, err := tx.PrepareContext(ctx, `UPDATE flights SET status=?, gate=?, terminal=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer updateStmt.Close()

		for _, f := range updates {
			if _, err := updateStmt.ExecContext(ctx, f.Status, f.Gate, f.Terminal, f.UID); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	if len(inserts) > 0 {
		insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO flights (uid, provider, direction, flight_code, city, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer insertStmt.Close()

		for _, f := range inserts {
			if _, err := insertStmt.ExecContext(ctx, f.UID, f.Provider, f.Direction, f.Code, f.City, f.SchedTime, f.Status, f.Gate, f.Terminal); err != nil {
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
	updateQuery := `UPDATE flights SET status=?, gate=?, terminal=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`
	_, err := tx.ExecContext(ctx, updateQuery, f.Status, f.Gate, f.Terminal, f.UID)
	if err != nil {
		return fmt.Errorf("tx update error: %w", err)
	}
	return nil
}

func (r *Repository) InsertFlight(ctx context.Context, tx *sql.Tx, f model.Flight) error {
	insertQuery := `INSERT INTO flights (provider, uid, direction, flight_code, city, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := tx.ExecContext(ctx, insertQuery, f.Provider, f.UID, f.Direction, f.Code, f.City, f.SchedTime, f.Status, f.Gate, f.Terminal)
	if err != nil {
		return fmt.Errorf("tx insert error: %w", err)
	}
	return nil
}
