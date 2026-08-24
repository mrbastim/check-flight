package monitor

import (
	"context"
	"log"
	"time"

	"check-flight/internal/bot"
	"check-flight/internal/model"
	"check-flight/internal/provider"
	"check-flight/internal/ui"
)

type Repository interface {
	LoadFlights(ctx context.Context) (map[string]model.Flight, error)
	SaveChanges(ctx context.Context, updates []model.Flight, inserts []model.Flight) error
}

type Service struct {
	repo     Repository
	provider provider.Provider
	query    model.Query
	bot      *bot.Bot
	print    bool
	interval time.Duration
}

func NewService(repo Repository, p provider.Provider, query model.Query, bot *bot.Bot, print bool, interval time.Duration) *Service {
	return &Service{repo: repo, provider: p, query: query, bot: bot, print: print, interval: interval}
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

	savedFlights, err := s.repo.LoadFlights(ctx)
	if err != nil {
		log.Printf("ОШИБКА БД (Load): %v\n", err)
		return
	}

	var updates, inserts []model.Flight
	var changesCount int

	for _, f := range flights {
		dbFlight, exists := savedFlights[f.UID]

		if exists {
			if (f.Status != "" && f.Status != dbFlight.Status) ||
				(f.Gate != "" && f.Gate != dbFlight.Gate) ||
				(f.Terminal != "" && f.Terminal != dbFlight.Terminal) ||
				(f.BaggageBelt != "" && f.BaggageBelt != dbFlight.BaggageBelt) ||
				(f.CheckInDesk != "" && f.CheckInDesk != dbFlight.CheckInDesk) {
				changesCount++
				if s.print {
					ui.PrintAlert(f, dbFlight)
				}
				updates = append(updates, f)
				if s.bot != nil {
					s.bot.SendAlert(ctx, f, dbFlight)

					if bot.IsTerminalStatus(f.Status) && !bot.IsTerminalStatus(dbFlight.Status) {
						s.bot.HandleAutoShift(ctx, dbFlight)
					}
				}
			}
		} else {
			inserts = append(inserts, f)
		}
	}
	if len(updates) > 0 || len(inserts) > 0 {
		if err := s.repo.SaveChanges(ctx, updates, inserts); err != nil {
			log.Println("ОШИБКА БД (Save Changes):", err)
		}
	}
	log.Printf("Опрос завершен. Найдено изменений: %d\n", changesCount)
}
