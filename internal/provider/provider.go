package provider

import (
	"context"

	"check-flight/internal/model"
)

// Query contains common provider-agnostic filters.
type Query struct {
	Direction string
	Search    string
	Terminal  string
}

// Provider fetches flights from an airport/API and returns normalized entities.
type Provider interface {
	ID() string
	Fetch(ctx context.Context, query Query) ([]model.Flight, error)
}
