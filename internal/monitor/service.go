package monitor

import (
	"context"
	"database/sql"
	"log"
	"time"

	"check-flight/internal/provider"
	"check-flight/internal/store"
	"check-flight/internal/ui"
)

type Service struct {
	db       *sql.DB
	provider provider.Provider
	query    provider.Query
	print    bool
	interval time.Duration
}

func NewService(db *sql.DB, p provider.Provider, query provider.Query, print bool, interval time.Duration) *Service {
	return &Service{db: db, provider: p, query: query, print: print, interval: interval}
}

func (s *Service) Start(ctx context.Context) {
	log.Printf("Параметры запуска -> Провайдер: '%s', Направление: '%s', Поиск: '%s', Терминал: '%s'\n", s.provider.ID(), s.query.Direction, s.query.Search, s.query.Terminal)

	s.process(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.process(ctx)
		}
	}
}

func (s *Service) process(ctx context.Context) {
	log.Printf("Начат опрос API (%s)...", s.provider.ID())

	flights, err := s.provider.Fetch(ctx, s.query)
	if err != nil {
		log.Printf("ОШИБКА API (%s): %v\n", s.provider.ID(), err)
		return
	}

	log.Printf("Успешно получено рейсов: %d\n", len(flights))

	savedFlights, err := store.LoadFlights(s.db)
	if err != nil {
		log.Printf("ОШИБКА БД (Load): %v\n", err)
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		log.Println("ОШИБКА БД (Begin Tx):", err)
		return
	}

	changesCount := 0

	for _, f := range flights {
		dbFlight, exists := savedFlights[f.UID]

		if exists {
			statusChanged := f.Status != "" && f.Status != dbFlight.Status
			gateChanged := f.Gate != "" && f.Gate != dbFlight.Gate

			if statusChanged || gateChanged {
				changesCount++

				if s.print {
					ui.PrintAlert(f, dbFlight)
				}

				log.Printf("[%s]: Рейс %s (%s). Статус: '%s' -> '%s' | Гейт: '%s' -> '%s'",
					f.UID, f.Code, f.Destination,
					dbFlight.Status, f.Status,
					dbFlight.Gate, f.Gate)

				if err := store.UpdateFlight(tx, f); err != nil {
					log.Printf("ОШИБКА БД (Update %s): %v\n", f.UID, err)
				}
			}
		} else {
			if err := store.InsertFlight(tx, f); err != nil {
				log.Printf("ОШИБКА БД (Insert %s): %v\n", f.UID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		log.Println("ОШИБКА БД (Commit Tx):", err)
	}

	log.Printf("Опрос завершен. Найдено изменений: %d\n", changesCount)
}
