package provider

import (
	"context"

	"check-flight/internal/model"
)

// Provider fetches flights from an airport/API and returns normalized entities.
type Provider interface {
	ID() string
	Fetch(ctx context.Context, query model.Query) ([]model.Flight, error)
}
