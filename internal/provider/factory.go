package provider

import (
	"fmt"
	"strings"

	"check-flight/internal/provider/svo"
)

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	registry := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, flightProvider := range providers {
		registry.providers[strings.ToLower(strings.TrimSpace(flightProvider.ID()))] = flightProvider
	}
	return registry
}

func (r *Registry) Get(id string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	flightProvider, ok := r.providers[strings.ToLower(strings.TrimSpace(id))]
	return flightProvider, ok
}

func New(id string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "svo":
		return svo.New(), nil
	default:
		return nil, fmt.Errorf("неизвестный провайдер: %s", id)
	}
}

func ParseProviderNameByID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "svo":
		return "Шереметьево"
	default:
		return id
	}
}
