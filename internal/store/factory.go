package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
)

// Open creates and initializes the configured storage backend.
func Open(driverName, dsn string) (*sql.DB, Storage, error) {
	driverName = strings.ToLower(strings.TrimSpace(driverName))
	if driverName == "" {
		driverName = "sqlite"
	}

	sqlDriver := driverName
	switch driverName {
	case "sqlite", "sqlite3":
		sqlDriver = "sqlite3"
		if dsn == "" {
			dsn = "./flights.db"
		}
	case "postgres", "postgresql", "pgx":
		sqlDriver = "pgx"
		if dsn == "" {
			return nil, nil, fmt.Errorf("DATABASE_URL is required for postgres")
		}
	default:
		return nil, nil, fmt.Errorf("unsupported database driver %q", driverName)
	}

	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s database: %w", driverName, err)
	}

	var repository Storage
	var initializer interface{ Init() error }
	if sqlDriver == "pgx" {
		postgresRepository := NewPostgresRepository(db)
		repository = postgresRepository
		initializer = postgresRepository
	} else {
		sqliteRepository := NewRepository(db)
		repository = sqliteRepository
		initializer = sqliteRepository
	}

	if err := initializer.Init(); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return db, repository, nil
}
