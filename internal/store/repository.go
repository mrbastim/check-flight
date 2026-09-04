package store

import (
	"context"

	"check-flight/internal/model"
)

// Storage is the persistence contract used by the bot and monitor.
type Storage interface {
	LoadFlights(ctx context.Context) (map[string]model.Flight, error)
	SaveChanges(ctx context.Context, updates []model.Flight, inserts []model.Flight) error

	GetOldSubscriptions(ctx context.Context) ([]OldSub, error)
	GetFlightByUID(ctx context.Context, uid string) (*model.Flight, error)
	GetNextFlight(ctx context.Context, code, afterTime string) (*model.Flight, error)
	SwitchSubscriptionUID(ctx context.Context, chatID int64, flightCode, newUID string) error
	DeleteOldFlights(ctx context.Context) (int64, error)

	FindUpcomingFlights(ctx context.Context, rawCode string) ([]model.Flight, error)
	GetFlightsByCity(ctx context.Context, city string) ([]model.Flight, error)
	RemoveSubscription(ctx context.Context, chatID int64, flightCode string) error
	GetUserSubscriptionsList(ctx context.Context, chatID int64) ([]model.Flight, error)
	SubscribeToUID(ctx context.Context, chatID int64, code, uid string) error
	GetSubscribersForUID(ctx context.Context, uid string) ([]int64, error)
}
