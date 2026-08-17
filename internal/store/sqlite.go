package store

import (
	"context"
	"database/sql"
	"fmt"

	"check-flight/internal/model"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return Repository{db: db}
}

func (r *Repository) Init(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS flights (
		uid TEXT PRIMARY KEY,
		flight_code TEXT,
		destination TEXT,
		sched_time DATETIME,
		status TEXT,
		gate TEXT,
		terminal TEXT,
		updated_at DATETIME
	);`
	_, err := db.Exec(query)
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) LoadFlights(ctx context.Context) (map[string]model.Flight, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT uid, flight_code, destination, sched_time, status, gate, terminal FROM flights")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]model.Flight)
	for rows.Next() {
		var f model.Flight
		if err := rows.Scan(&f.UID, &f.Code, &f.Destination, &f.SchedTime, &f.Status, &f.Gate, &f.Terminal); err != nil {
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
		insertStmt, err := tx.PrepareContext(ctx, `INSERT INTO flights (uid, flight_code, destination, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer insertStmt.Close()

		for _, f := range inserts {
			if _, err := insertStmt.ExecContext(ctx, f.UID, f.Code, f.Destination, f.SchedTime, f.Status, f.Gate, f.Terminal); err != nil {
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
	insertQuery := `INSERT INTO flights (uid, flight_code, destination, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := tx.ExecContext(ctx, insertQuery, f.UID, f.Code, f.Destination, f.SchedTime, f.Status, f.Gate, f.Terminal)
	if err != nil {
		return fmt.Errorf("tx insert error: %w", err)
	}
	return nil
}
