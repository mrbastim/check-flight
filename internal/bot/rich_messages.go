package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"check-flight/internal/provider"
)

// Генерация HTML страницы для Rich Message
func (b *Bot) buildRichSearchPage(ctx context.Context, token, direction string, page int) (string, tgbotapi.InlineKeyboardMarkup, error) {
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
		return "<h3>Рейсы не найдены</h3>", tgbotapi.InlineKeyboardMarkup{}, nil
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

		actionLink := GetInfoURL(b.api.Self.UserName, f.Code)

		var externalLink string
		if provider, ok := b.providers.Get(f.Provider); ok {
			externalLink = provider.GetFlightURL(f.InternalID, f.Direction)
		}

		infoStr := html.EscapeString(f.Status)
		checkInDesk := ""
		if IsCheckInStatus(f.Status) {
			checkInDesk = f.CheckInDesk
		}
		baggageBelt := ""
		if IsBaggageClaimStatus(f.Status) {
			baggageBelt = f.BaggageBelt
		}

		data.Flights = append(data.Flights, flightRenderData{
			Icon:        icon,
			Provider:    provider.ParseProviderNameByID(f.Provider),
			Dest:        dest,
			Code:        f.Code,
			Arrow:       arrow,
			Terminal:    f.Terminal,
			CheckInDesk: checkInDesk,
			BaggageBelt: baggageBelt,
			Date:        t.Format("02.01"),
			Time:        t.Format("15:04"),
			UID:         f.UID,
			Info:        template.HTML(infoStr),
			ActionURL:   actionLink,
			ExternalURL: externalLink,
		})
	}

	markup := b.buildSearchKeyboard(token, direction, page, pageCount)

	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, "search_response.html", data); err != nil {
		return "", markup, err
	}
	return buf.String(), markup, nil
}

// Вспомогательная отправка Rich Message
func (b *Bot) sendRichMessage(chatID int64, html string, markup tgbotapi.InlineKeyboardMarkup) {
	type inputRichMessage struct {
		HTML string `json:"html"`
	}
	richMsgBytes, _ := json.Marshal(inputRichMessage{HTML: html})
	markupBytes, _ := json.Marshal(markup)

	params := tgbotapi.Params{
		"chat_id":      strconv.FormatInt(chatID, 10),
		"rich_message": string(richMsgBytes),
		"reply_markup": string(markupBytes), // Прикрепляем клавиатуру
	}

	if _, err := b.api.MakeRequest("sendRichMessage", params); err != nil {
		log.Printf("Ошибка отправки: %v", err)
	}
}

// Редактирование существующего Rich Message (при перелистывании страниц)
func (b *Bot) editRichMessage(chatID int64, msgID int, html string, markup tgbotapi.InlineKeyboardMarkup) {
	type inputRichMessage struct {
		HTML string `json:"html"`
	}
	richMsgBytes, _ := json.Marshal(inputRichMessage{HTML: html})
	markupBytes, _ := json.Marshal(markup)

	params := tgbotapi.Params{
		"chat_id":      strconv.FormatInt(chatID, 10),
		"message_id":   strconv.Itoa(msgID),
		"rich_message": string(richMsgBytes),
		"reply_markup": string(markupBytes),
	}

	// Обновляем сообщение (API Telegram позволяет передавать rich_message в editMessageText)
	if _, err := b.api.MakeRequest("editMessageText", params); err != nil {
		log.Printf("Ошибка обновления Rich Message: %v", err)
	}
}
