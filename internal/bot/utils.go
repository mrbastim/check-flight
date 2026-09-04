package bot

import (
	"check-flight/internal/model"
	"fmt"
	"strings"
)

func filterFlights(flights []model.Flight, direction string) []model.Flight {
	if direction == "all" {
		return flights
	}
	var filtered []model.Flight
	for _, f := range flights {
		if f.Direction == direction {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func filterLabel(value, current, label string) string {
	if value == current {
		return "• " + label
	}
	return label
}

// IsTerminalStatus проверяет, завершен ли рейс
func IsTerminalStatus(status string) bool {
	s := strings.ToLower(status)
	s = strings.ReplaceAll(s, "ё", "е")
	return strings.Contains(s, "прибыл") || strings.Contains(s, "посадку") ||
		strings.Contains(s, "вылетел") || strings.Contains(s, "отправлен") ||
		strings.Contains(s, "отменен")
}

func InFlightStatus(status string) bool {
	s := strings.ToLower(status)
	s = strings.ReplaceAll(s, "ё", "е")
	return strings.Contains(s, "в полете")
}

func GetSubscribeURL(botUsername, flightCode string) string {
	return strings.ReplaceAll(fmt.Sprintf(subscribeCommandURL, botUsername, flightCode), " ", "%20")
}

func GetUnsubscribeURL(botUsername, flightCode string) string {
	return strings.ReplaceAll(fmt.Sprintf(unsubscribeCommandURL, botUsername, flightCode), " ", "%20")
}

func GetInfoURL(botUsername, flightCode string) string {
	return strings.ReplaceAll(fmt.Sprintf(infoCommandURL, botUsername, flightCode), " ", "%20")
}
