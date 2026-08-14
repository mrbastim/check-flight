package store

import (
	"database/sql"
	"fmt"

	"check-flight/internal/model"
)

func Init(db *sql.DB) error {
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
		return fmt.Errorf("ошибка создания таблицы: %w", err)
	}
	return nil
}

func LoadFlights(db *sql.DB) (map[string]model.Flight, error) {
	rows, err := db.Query("SELECT uid, flight_code, destination, sched_time, status, gate, terminal FROM flights")
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения рейсов из бд: %w", err)
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
		return nil, fmt.Errorf("ошибка итерации по рейсам: %w", err)
	}
	return res, nil
}

func UpdateFlight(tx *sql.Tx, f model.Flight) error {
	updateQuery := `UPDATE flights SET status=?, gate=?, terminal=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`
	_, err := tx.Exec(updateQuery, f.Status, f.Gate, f.Terminal, f.UID)
	if err != nil {
		return fmt.Errorf("ошибка обновления рейса %s: %w", f.UID, err)
	}
	return nil
}

func InsertFlight(tx *sql.Tx, f model.Flight) error {
	insertQuery := `INSERT INTO flights (uid, flight_code, destination, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := tx.Exec(insertQuery, f.UID, f.Code, f.Destination, f.SchedTime, f.Status, f.Gate, f.Terminal)
	if err != nil {
		return fmt.Errorf("ошибка вставки рейса %s: %w", f.UID, err)
	}
	return nil
}
