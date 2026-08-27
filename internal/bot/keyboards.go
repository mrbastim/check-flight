package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"check-flight/internal/model"
)

const flightsPerPage = 8

func (b *Bot) newSearch(city string) string {
	token := strconv.FormatUint(atomic.AddUint64(&b.searchSeq, 1), 10)
	b.searchMu.Lock()
	if b.searches == nil {
		b.searches = make(map[string]string)
	}
	b.searches[token] = city
	b.searchMu.Unlock()
	return token
}

func (b *Bot) searchCity(token string) (string, bool) {
	b.searchMu.RLock()
	city, ok := b.searches[token]
	b.searchMu.RUnlock()
	return city, ok
}

func (b *Bot) sendFlightSearch(ctx context.Context, chatID int64, city string) {
	token := b.newSearch(city)
	text, markup, err := b.flightSearchPage(ctx, token, "all", 0)
	if err != nil {
		b.send(chatID, fmt.Sprintf("❌ Ошибка при поиске рейсов по городу *%s*", city))
		log.Printf("Ошибка поиска рейсов по городу %s: %v", city, err)
		return
	}
	if text == "" {
		b.send(chatID, fmt.Sprintf("Рейсы по городу *%s* не найдены в расписании на ближайшие дни.", city))
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = markup
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("Ошибка при отправке результатов поиска: %v", err)
	}
}

func (b *Bot) flightSearchPage(ctx context.Context, token, direction string, page int) (string, tgbotapi.InlineKeyboardMarkup, error) {
	city, ok := b.searchCity(token)
	if !ok {
		return "", tgbotapi.InlineKeyboardMarkup{}, fmt.Errorf("поиск устарел")
	}

	flights, err := b.repo.GetFlightsByCity(ctx, city)
	if err != nil {
		return "", tgbotapi.InlineKeyboardMarkup{}, err
	}
	flights = filterFlights(flights, direction)
	if len(flights) == 0 {
		return "", tgbotapi.InlineKeyboardMarkup{}, nil
	}

	pageCount := (len(flights) + flightsPerPage - 1) / flightsPerPage
	if page < 0 {
		page = 0
	}
	if page >= pageCount {
		page = pageCount - 1
	}
	start := page * flightsPerPage
	end := start + flightsPerPage
	if end > len(flights) {
		end = len(flights)
	}

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, flightsPerPage+2)
	for _, flight := range flights[start:end] {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(flightButton(flight)))
	}
	rows = append(rows, filterButtons(token, direction, page))
	if pageCount > 1 {
		rows = append(rows, paginationButtons(token, direction, page, pageCount))
	}

	directionTitle := map[string]string{"all": "Все рейсы", "dep": "Вылеты", "arr": "Прилеты"}[direction]
	text := fmt.Sprintf("🔎 *Рейсы по городу %s*\n%s\nСтраница %d из %d:", city, directionTitle, page+1, pageCount)
	return text, tgbotapi.NewInlineKeyboardMarkup(rows...), nil
}

func filterFlights(flights []model.Flight, direction string) []model.Flight {
	if direction == "all" {
		return flights
	}
	filtered := make([]model.Flight, 0, len(flights))
	for _, flight := range flights {
		if flight.Direction == direction {
			filtered = append(filtered, flight)
		}
	}
	return filtered
}

func flightButton(flight model.Flight) tgbotapi.InlineKeyboardButton {
	t, _ := time.Parse(time.RFC3339, flight.SchedTime)
	icon := "🛫"
	fromTo := fmt.Sprintf("%s ➔ %s", strings.ToUpper(flight.Provider), flight.City)
	if flight.Direction == "arr" {
		icon = "🛬"
		fromTo = fmt.Sprintf("%s ➔ %s", flight.City, strings.ToUpper(flight.Provider))
	}
	text := fmt.Sprintf("%s %s | %s | %s", icon, fromTo, flight.Code, t.Format("02.01 15:04"))
	return tgbotapi.NewInlineKeyboardButtonData(text, "info:"+flight.UID)
}

func filterButtons(token, direction string, page int) []tgbotapi.InlineKeyboardButton {
	return []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData(filterLabel("all", direction, "Все"), fmt.Sprintf("fl:%s:all:%d", token, page)),
		tgbotapi.NewInlineKeyboardButtonData(filterLabel("dep", direction, "Вылеты"), fmt.Sprintf("fl:%s:dep:%d", token, page)),
		tgbotapi.NewInlineKeyboardButtonData(filterLabel("arr", direction, "Прилеты"), fmt.Sprintf("fl:%s:arr:%d", token, page)),
	}
}

func filterLabel(value, current, label string) string {
	if value == current {
		return "• " + label
	}
	return label
}

func paginationButtons(token, direction string, page, pageCount int) []tgbotapi.InlineKeyboardButton {
	buttons := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if page > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("← Назад", fmt.Sprintf("pg:%s:%s:%d", token, direction, page-1)))
	}
	if page+1 < pageCount {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Вперед →", fmt.Sprintf("pg:%s:%s:%d", token, direction, page+1)))
	}
	return buttons
}
