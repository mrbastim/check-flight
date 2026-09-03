package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"check-flight/internal/provider"
)

// Генерация HTML страницы для Rich Message
func (b *Bot) buildRichSearchPage(ctx context.Context, token, direction string, page int) (string, error) {
	city, ok := b.searchCity(token)
	if !ok {
		return "", fmt.Errorf("поиск устарел")
	}

	flights, err := b.repo.GetFlightsByCity(ctx, city)
	if err != nil {
		return "", err
	}
	flights = filterFlights(flights, direction)
	if len(flights) == 0 {
		return "<h3>Рейсы не найдены</h3>", nil
	}

	pageCount := (len(flights) + flightsPerPage - 1) / flightsPerPage
	if page < 0 {
		page = 0
	}
	if page >= pageCount && pageCount > 0 {
		page = pageCount - 1
	}

	start := page * flightsPerPage
	end := start + flightsPerPage
	if end > len(flights) {
		end = len(flights)
	}

	data := searchTemplateData{
		City:           cases.Title(language.Und, cases.NoLower).String(city),
		DirectionTitle: map[string]string{"all": "Все рейсы", "dep": "Вылеты", "arr": "Прилеты"}[direction],
		Page:           page + 1,
		PageCount:      pageCount,
		Token:          token,
		CurrentDir:     direction,
		FilterAll:      filterLabel("all", direction, "Все"),
		FilterDep:      filterLabel("dep", direction, "Вылеты"),
		FilterArr:      filterLabel("arr", direction, "Прилеты"),
		HasPrev:        page > 0,
		PrevPage:       page - 1,
		HasNext:        page+1 < pageCount,
		NextPage:       page + 1,
	}

	for _, f := range flights[start:end] {
		t, _ := time.Parse(time.RFC3339, f.SchedTime)
		icon, dest, arrow := "🛫", f.City, "→"
		if f.Direction == "arr" {
			icon, dest, arrow = "🛬", f.City, "←"
		}

		data.Flights = append(data.Flights, flightRenderData{
			Icon:     icon,
			Provider: provider.ParseProviderNameByID(f.Provider),
			Dest:     dest,
			Code:     f.Code,
			Arrow:    arrow,
			Date:     t.Format("02.01"),
			Time:     t.Format("15:04"),
			UID:      f.UID,
		})
	}

	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, "search_response.html", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Вспомогательная отправка Rich Message
func (b *Bot) sendRichMessage(chatID int64, html string) {
	type inputRichMessage struct {
		HTML string `json:"html"`
	}
	richMsgBytes, _ := json.Marshal(inputRichMessage{HTML: html})

	params := tgbotapi.Params{
		"chat_id":      strconv.FormatInt(chatID, 10),
		"rich_message": string(richMsgBytes),
	}

	if _, err := b.api.MakeRequest("sendRichMessage", params); err != nil {
		log.Printf("Ошибка отправки Rich Message: %v", err)
	}
}

// Редактирование существующего Rich Message (при перелистывании страниц)
func (b *Bot) editRichMessage(chatID int64, msgID int, html string) {
	type inputRichMessage struct {
		HTML string `json:"html"`
	}
	richMsgBytes, _ := json.Marshal(inputRichMessage{HTML: html})

	params := tgbotapi.Params{
		"chat_id":      strconv.FormatInt(chatID, 10),
		"message_id":   strconv.Itoa(msgID),
		"rich_message": string(richMsgBytes),
	}

	// Обновляем сообщение (API Telegram позволяет передавать rich_message в editMessageText)
	if _, err := b.api.MakeRequest("editMessageText", params); err != nil {
		log.Printf("Ошибка обновления Rich Message: %v", err)
	}
}
