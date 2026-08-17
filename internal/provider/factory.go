package provider

import (
	"fmt"
	"strings"

	"check-flight/internal/provider/svo"
)

func New(id string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "svo":
		return svo.New(), nil
	default:
		return nil, fmt.Errorf("неизвестный провайдер: %s", id)
	}
}
