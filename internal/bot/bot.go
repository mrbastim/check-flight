package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"check-flight/internal/model"
	"check-flight/internal/provider"
	"check-flight/internal/store"
)

type Bot struct {
	api       *tgbotapi.BotAPI
	repo      store.Storage
	providers *provider.Registry
	searchMu  sync.RWMutex
	searches  map[string]string
	searchSeq uint64
	tmpl      *template.Template
}

type searchTemplateData struct {
	City           string
	DirectionTitle string
	Page           int
	PageCount      int
	Token          string
	CurrentDir     string
	FilterAll      string
	FilterDep      string
	FilterArr      string
	HasPrev        bool
	PrevPage       int
	HasNext        bool
	NextPage       int
	Flights        []flightRenderData
}

type flightRenderData struct {
	Icon        string
	Provider    string
	Dest        string
	Code        string
	Arrow       string
	Terminal    string
	Gate        template.HTML
	CheckInDesk template.HTML
	BaggageBelt template.HTML
	Date        string
	Time        template.HTML
	UID         string
	Info        template.HTML
	ActionURL   string
	ExternalURL string
}

const (
	subscribeCommandPattern = "/track %s"
	subscribeCommandURL     = "https://t.me/%s?text=/track %s"

	unsubscribeCommandPattern = "/untrack %s"
	unsubscribeCommandURL     = "https://t.me/%s?text=/untrack %s"

	listCommandURL = "https://t.me/%s?text=/list"
	helpCommandURL = "https://t.me/%s?text=/help"

	infoCommandPattern = "/info %s"
	infoCommandURL     = "https://t.me/%s?text=/info %s"

	searchCommandPattern = "/search %s"
	searchCommandURL     = "https://t.me/%s?text=/search %s"
)

const flightsPerPage = 5

func New(token string, repo store.Storage, providers *provider.Registry) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram auth error: %w", err)
	}

	tmpl, err := template.ParseGlob("internal/bot/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("template parse error: %w", err)
	}

	log.Printf("Авторизован Telegram-бот: @%s", api.Self.UserName)
	return &Bot{api: api, repo: repo, providers: providers, searches: make(map[string]string), tmpl: tmpl}, nil
}

func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if update.CallbackQuery != nil {
				b.handleCallback(ctx, update.CallbackQuery)
				continue
			}
			if update.Message == nil || !update.Message.IsCommand() {
				continue
			}
			b.handleCommand(ctx, update.Message)
		}
	}
}

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	args := strings.TrimSpace(msg.CommandArguments())

	switch msg.Command() {
	case "start", "help":
		text := fmt.Sprintf("✈️ *Бот отслеживания авиарейсов*\n\n"+
			"• [/track SU 1234](%s) — подписаться на рейс\n"+
			"• [/untrack SU 1234](%s) — отписаться\n"+
			"• [/list](%s) — мои подписки\n"+
			"• [/info SU 1234](%s) — информация по рейсу\n"+
			"Для поиска рейсов по городу вылета/прилета используйте команду [/search](%s)\n",
			GetSubscribeURL(b.api.Self.UserName, ""),
			GetUnsubscribeURL(b.api.Self.UserName, ""),
			listCommandURL, fmt.Sprintf(infoCommandURL, b.api.Self.UserName, ""), fmt.Sprintf(searchCommandURL, b.api.Self.UserName, ""))
		b.send(chatID, text)
		return
	case "track":
		if args == "" {
			b.send(chatID, fmt.Sprintf("❌ Укажите номер рейса. Пример: [/track SU 1234](%s)", GetSubscribeURL(b.api.Self.UserName, "")))
			return
		}

		code := store.NormalizeFlightCode(args)

		flights, err := b.repo.FindUpcomingFlights(ctx, code)
		if err != nil || len(flights) == 0 {
			b.send(chatID, fmt.Sprintf("Рейс *%s* не найден в расписании на ближайшие дни. Возможно, он появится позже.", code))
			return
		}

		var rows [][]tgbotapi.InlineKeyboardButton
		for _, f := range flights {
			t, _ := time.Parse(time.RFC3339, f.SchedTime)
			statusIcon := "✈️"
			if IsTerminalStatus(f.Status) {
				statusIcon = "✅"
			}

			// "✈️ 21.08 16:30 (Ожидается)"
			btnText := fmt.Sprintf("%s %s %s (%s)", statusIcon, t.Format("02.01"), t.Format("15:04"), f.Status)

			cbData := "sub:" + f.UID // ! cbData ограничен 64 байтами
			btn := tgbotapi.NewInlineKeyboardButtonData(btnText, cbData)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}

		msgOut := tgbotapi.NewMessage(chatID, fmt.Sprintf("📅 Выберите дату вылета для рейса *%s*:", code))
		msgOut.ParseMode = "Markdown"
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		_, err = b.api.Send(msgOut)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения: %v", err)
		}

	case "untrack":
		if args == "" {
			b.send(chatID, fmt.Sprintf("❌ Укажите номер рейса. Пример: [/untrack SU 1234](%s)", GetUnsubscribeURL(b.api.Self.UserName, "")))
			return
		}
		_ = b.repo.RemoveSubscription(ctx, chatID, args)
		b.send(chatID, fmt.Sprintf("🗑 Вы отписались от рейса *%s*", store.NormalizeFlightCode(args)))

	case "list":
		subs, _ := b.repo.GetUserSubscriptionsList(ctx, chatID)
		if len(subs) == 0 {
			b.send(chatID, "📭 У вас пока нет активных подписок.")
			return
		}
		var sb strings.Builder
		sb.WriteString("📋 *Ваши подписки:*\n\n")
		byProvider := make(map[string][]model.Flight)
		for _, f := range subs {
			byProvider[f.Provider] = append(byProvider[f.Provider], f)
		}
		for providerID, flights := range byProvider {
			provider := provider.ParseProviderNameByID(providerID)
			if provider == "" {
				provider = "Другой"
			}
			sb.WriteString(fmt.Sprintf("*%s:*\n", provider))
			for _, f := range flights {
				t, _ := time.Parse(time.RFC3339, f.SchedTime)
				icon := "🛫"
				fromTo := fmt.Sprintf("%s ➔ %s", strings.ToUpper(f.Provider), f.City)
				if f.Direction == "arr" {
					icon = "🛬"
					fromTo = fmt.Sprintf("%s ➔ %s", f.City, strings.ToUpper(f.Provider))
				}
				sb.WriteString(fmt.Sprintf("%s %s | %s | %s", icon, fromTo, f.Code, t.Format("02.01 15:04")))
			}
		}
		b.send(chatID, sb.String())

	case "info":
		if args == "" {
			b.send(chatID, fmt.Sprintf("❌ Укажите номер рейса. Пример: [/info SU 1234](%s)", fmt.Sprintf(infoCommandURL, b.api.Self.UserName, "")))
			return
		}
		code := store.NormalizeFlightCode(args)
		flights, err := b.repo.FindUpcomingFlights(ctx, code)
		if err != nil || len(flights) == 0 {
			b.send(chatID, fmt.Sprintf("Рейс *%s* не найден в расписании на ближайшие дни.", code))
			return
		}
		var sb strings.Builder
		var flightURL string
		sb.WriteString(fmt.Sprintf("ℹ️ *Информация по рейсу* `%s`*:*\nГород: %s\n\n", code, flights[0].City))
		for _, f := range flights {
			t, _ := time.Parse(time.RFC3339, f.SchedTime)
			if flightProvider, ok := b.providers.Get(f.Provider); ok {
				flightURL = flightProvider.GetFlightURL(f.InternalID, f.Direction)
			}
			if f.Direction == "arr" {
				sb.WriteString(fmt.Sprintf("🛬 *Прилет:* %s | Терминал %s\n", t.Format("02.01 15:04"), f.Terminal))
				if f.Status != "" {
					sb.WriteString(fmt.Sprintf("ℹ️ *Статус:* %s\n", f.Status))
				}
				if f.BaggageBelt != "" {
					sb.WriteString(fmt.Sprintf("🧳 *Багажная лента:* %s\n", f.BaggageBelt))
				}
			} else {
				sb.WriteString(fmt.Sprintf("🛫 *Вылет:* %s | Терминал %s\n", t.Format("02.01 15:04"), f.Terminal))
				if f.Status != "" {
					sb.WriteString(fmt.Sprintf("ℹ️ *Статус:* %s\n", f.Status))
				}
				if f.Gate != "" && !InFlightStatus(f.Status) {
					sb.WriteString(fmt.Sprintf("🚪 *Выход на посадку:* %s\n", f.Gate))
				}
			}
			sb.WriteString(fmt.Sprintf("🔗 [Подробнее](%s)\n", flightURL))
			sb.WriteString(fmt.Sprintf("🔗 [Подписаться](%s)\n\n", GetSubscribeURL(b.api.Self.UserName, code)))
		}
		b.send(chatID, sb.String())

	case "search":
		if args == "" {
			b.send(chatID, fmt.Sprintf("❌ Укажите город вылета или прилета. Пример: [/search Москва](%s)", fmt.Sprintf(searchCommandURL, b.api.Self.UserName, "")))
			return
		}
		city := normalizeSearchCity(args)
		flights, err := b.repo.GetFlightsByCity(ctx, city)
		if err != nil {
			log.Printf("Ошибка поиска рейсов по городу %s: %v", city, err)
			return
		}
		if len(flights) == 0 {
			b.send(chatID, fmt.Sprintf("Рейсы по городу *%s* не найдены в расписании на ближайшие дни.", city))
			return
		}

		b.sendFlightSearch(ctx, chatID, city)

	default:
		b.send(chatID, fmt.Sprintf("❌ Неизвестная команда. Используйте [/help](%s) для списка доступных команд.", fmt.Sprintf(helpCommandURL, b.api.Self.UserName)))
	}
}

func normalizeSearchCity(city string) string {
	return cases.Title(language.Und, cases.NoLower).String(strings.TrimSpace(city))
}

// Обработка нажатия на кнопку (выбор даты)
func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if strings.HasPrefix(cb.Data, "fl:") || strings.HasPrefix(cb.Data, "pg:") || strings.HasPrefix(cb.Data, "ref:") {
		parts := strings.Split(cb.Data, ":")
		if len(parts) != 4 {
			return
		}
		page, err := strconv.Atoi(parts[3])
		if err != nil {
			return
		}
		htmlData, markup, err := b.buildRichSearchPage(ctx, parts[1], parts[2], page)
		if err != nil {
			b.answerCallback(cb.ID, "Результаты поиска устарели")
			log.Printf("Ошибка при построении Rich Message %s: %v", parts[1], err)
			return
		}
		b.editRichMessage(cb.Message.Chat.ID, cb.Message.MessageID, htmlData, markup)
		b.answerCallback(cb.ID, "")
		return
	}

	if strings.HasPrefix(cb.Data, "sub:") {
		uid := strings.TrimPrefix(cb.Data, "sub:")

		flight, err := b.repo.GetFlightByUID(ctx, uid)
		if err != nil {
			log.Printf("Ошибка при получении рейса по UID %s: %v", uid, err)
			_, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Рейс не найден!"))
			if err != nil {
				log.Printf("Ошибка при отправке колбека: %v", err)
			}
			return
		}

		if err = b.repo.SubscribeToUID(ctx, cb.Message.Chat.ID, flight.Code, uid); err != nil {
			log.Printf("Ошибка при подписке на рейс %s для чата %d: %v", flight.Code, cb.Message.Chat.ID, err)
			_, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Произошла ошибка."))
			if err != nil {
				log.Printf("Ошибка при отправке колбека: %v", err)
			}
			return
		}

		t, _ := time.Parse(time.RFC3339, flight.SchedTime)
		reply := fmt.Sprintf("✅ *Подписка оформлена!*\n\n📍 Направление: *%s*\n🕒 Вылет: *%s*\nℹ️ Статус: *%s*\n\n🔔 Вы будете получать уведомления в данном чате.",
			flight.City, t.Format("15:04 02.01"), flight.Status)

		editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, reply)
		editMsg.ParseMode = "Markdown"
		_, err = b.api.Send(editMsg)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения: %v", err)
		}

		// Сообщаем телеграму, что колбек обработан
		_, err = b.api.Request(tgbotapi.NewCallback(cb.ID, "Подписка успешна!"))
		if err != nil {
			log.Printf("Ошибка при отправке колбека: %v", err)
		}
	}
	if strings.HasPrefix(cb.Data, "info:") {
		uid := strings.TrimPrefix(cb.Data, "info:")

		flight, err := b.repo.GetFlightByUID(ctx, uid)
		if err != nil {
			log.Printf("Ошибка при получении рейса по UID %s: %v", uid, err)
			_, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "Рейс не найден!"))
			if err != nil {
				log.Printf("Ошибка при отправке колбека: %v", err)
			}
			return
		}

		var sb strings.Builder
		var flightURL string
		sb.WriteString(fmt.Sprintf("ℹ️ *Информация по рейсу* `%s`*:*\nГород: %s\n\n", flight.Code, flight.City))
		t, _ := time.Parse(time.RFC3339, flight.SchedTime)
		if flightProvider, ok := b.providers.Get(flight.Provider); ok {
			flightURL = flightProvider.GetFlightURL(flight.InternalID, flight.Direction)
		}
		if flight.Direction == "arr" {
			sb.WriteString(fmt.Sprintf("🛬 *Прилет:* %s | Терминал %s\n", t.Format("02.01 15:04"), flight.Terminal))
			if flight.Status != "" {
				sb.WriteString(fmt.Sprintf("ℹ️ *Статус:* %s\n", flight.Status))
			}
			if flight.BaggageBelt != "" {
				sb.WriteString(fmt.Sprintf("🧳 *Багажная лента:* %s\n", flight.BaggageBelt))
			}
		} else {
			sb.WriteString(fmt.Sprintf("🛫 *Вылет:* %s | Терминал %s\n", t.Format("02.01 15:04"), flight.Terminal))
			if flight.Status != "" {
				sb.WriteString(fmt.Sprintf("ℹ️ *Статус:* %s\n", flight.Status))
			}
			if flight.Gate != "" && !InFlightStatus(flight.Status) {
				sb.WriteString(fmt.Sprintf("🚪 *Выход на посадку:* %s\n", flight.Gate))
			}
		}
		sb.WriteString(fmt.Sprintf("🔗 [Подробнее](%s)\n", flightURL))
		sb.WriteString(fmt.Sprintf("🔗 [Подписаться](%s)\n\n", GetSubscribeURL(b.api.Self.UserName, flight.Code)))

		reply := sb.String()
		editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, reply)
		editMsg.ParseMode = "Markdown"
		_, err = b.api.Send(editMsg)
		if err != nil {
			log.Printf("Ошибка при отправке сообщения: %v", err)
		}
	}
}

func (b *Bot) answerCallback(callbackID, text string) {
	_, err := b.api.Request(tgbotapi.NewCallback(callbackID, text))
	if err != nil {
		log.Printf("Ошибка при отправке колбека: %v", err)
	}
}

// SendAlert рассылает уведомления
func (b *Bot) SendAlert(ctx context.Context, newF, oldF model.Flight) {
	chats, err := b.repo.GetSubscribersForUID(ctx, newF.UID)
	if err != nil {
		log.Printf("Ошибка при получении подписчиков для рейса %s: %v", newF.Code, err)
		return
	}
	if len(chats) == 0 {
		return
	}

	t, _ := time.Parse(time.RFC3339, newF.SchedTime)
	var changes []string

	if newF.Gate != oldF.Gate && newF.Gate != "" {
		oldG := oldF.Gate
		if oldG == "" {
			oldG = "Нет"
		}
		changes = append(changes, fmt.Sprintf("🚪 *Выход на посадку:* %s ➔ *%s*", oldG, newF.Gate))
	}
	if newF.Status != oldF.Status && newF.Status != "" {
		oldS := oldF.Status
		if oldS == "" {
			oldS = "По расписанию"
		}
		changes = append(changes, fmt.Sprintf("ℹ️ *Статус:* %s ➔ *%s*", oldS, newF.Status))
	}
	if newF.BaggageBelt != oldF.BaggageBelt && newF.BaggageBelt != "" && newF.Direction == "arr" {
		oldB := oldF.BaggageBelt
		if oldB == "" {
			oldB = "Нет"
		}
		changes = append(changes, fmt.Sprintf("🧳 *Багаж:* %s ➔ *%s*", oldB, newF.BaggageBelt))
	}
	if len(changes) == 0 {
		return
	}

	var flightURL string
	if flightProvider, ok := b.providers.Get(newF.Provider); ok {
		flightURL = flightProvider.GetFlightURL(newF.InternalID, newF.Direction)
	}

	msgText := fmt.Sprintf("🔔 *Обновление рейса %s*\n🌍 %s | 🕒 %s\n\n%s",
		newF.Code, newF.City, t.Format("02.01 15:04"), strings.Join(changes, "\n"))
	if flightURL != "" {
		msgText += fmt.Sprintf("\n\n🔗 [Подробнее](%s)\n", flightURL) +
			fmt.Sprintf("🔗 [Отписаться](%s)", GetUnsubscribeURL(b.api.Self.UserName, newF.Code))
	}

	for _, chatID := range chats {
		b.send(chatID, msgText)
	}
}

// SendShiftAlert уведомляет о завершении обслуживания рейса и переносе подписки на следующий рейс
func (b *Bot) SendShiftAlert(chatId int64, flightCode string, nextF model.Flight) {
	timeNext, _ := time.Parse(time.RFC3339, nextF.SchedTime)
	msgText := fmt.Sprintf("🏁 Обслуживание рейса *%s* за прошлую дату завершено.\n\n"+
		"♻️ Подписка перенесена на следующий рейс:\n📅 *%s* | ℹ️ %s",
		flightCode, timeNext.Format("02.01 15:04"), nextF.Status)

	b.send(chatId, msgText)
}

func (b *Bot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, err := b.api.Send(msg)
	if err != nil {
		log.Printf("Ошибка при отправке сообщения в чат %d: %v", chatID, err)
	}
}

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

// Отправка нового поиска
func (b *Bot) sendFlightSearch(ctx context.Context, chatID int64, city string) {
	token := b.newSearch(city)
	htmlData, markup, err := b.buildRichSearchPage(ctx, token, "all", 0)
	if err != nil {
		b.send(chatID, "❌ Ошибка при поиске рейсов или поиск устарел.")
		log.Printf("Ошибка при построении Rich Message для поиска рейсов по городу %s: %v", city, err)
		return
	}

	b.sendRichMessage(chatID, htmlData, markup)
}

func (b *Bot) sendSearchTable(chatID int64, city string, flights []model.Flight) {
	data := searchTemplateData{
		City: cases.Title(language.Und, cases.NoLower).String(city),
	}

	for _, f := range flights {
		t, _ := time.Parse(time.RFC3339, f.SchedTime)
		icon := "🛫"
		dest := f.City
		if f.Direction == "arr" {
			icon = "🛬"
			dest = f.City // = "Прилет"
		}

		data.Flights = append(data.Flights, flightRenderData{
			Icon:     icon,
			Provider: cases.Title(language.Und, cases.NoLower).String(f.Provider),
			Dest:     dest,
			Code:     f.Code,
			Terminal: f.Terminal,
			Date:     t.Format("02.01"),
			Time:     template.HTML(t.Format("15:04")),
			UID:      f.UID,
		})
	}

	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, "search_response.html", data); err != nil {
		log.Printf("Ошибка при рендеринге шаблона: %v", err)
		return
	}

	type inputRichMessage struct {
		HTML string `json:"html"`
	}

	richMsgBytes, err := json.Marshal(inputRichMessage{HTML: buf.String()})
	if err != nil {
		log.Printf("Ошибка JSON маршалинга rich_message: %v", err)
		return
	}

	params := tgbotapi.Params{"chat_id": strconv.FormatInt(chatID, 10), "rich_message": string(richMsgBytes)}
	_, err = b.api.MakeRequest("sendRichMessage", params)
	if err != nil {
		log.Printf("Ошибка при отправке сообщения: %v", err)
		b.send(chatID, "❌ Ваш клиент Telegram не поддерживает новый формат таблиц.")
	}
}
