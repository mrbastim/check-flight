package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"check-flight/internal/model"
	"check-flight/internal/provider"
	"check-flight/internal/store"
)

type Bot struct {
	api       *tgbotapi.BotAPI
	repo      *store.Repository
	providers *provider.Registry
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

func New(token string, repo *store.Repository, providers *provider.Registry) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram auth error: %w", err)
	}
	log.Printf("Авторизован Telegram-бот: @%s", api.Self.UserName)
	return &Bot{api: api, repo: repo, providers: providers}, nil
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
		b.api.Send(msgOut)

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
		flights, err := b.repo.GetFlightsByCity(ctx, args)
		if err != nil {
			b.send(chatID, fmt.Sprintf("❌ Ошибка при поиске рейсов по городу *%s*", args))
			log.Printf("Ошибка поиска рейсов по городу %s: %v", args, err)
			return
		}
		if len(flights) == 0 {
			b.send(chatID, fmt.Sprintf("Рейсы по городу *%s* не найдены в расписании на ближайшие дни.", args))
			return
		}
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, f := range flights {
			t, _ := time.Parse(time.RFC3339, f.SchedTime)

			// Формат: "🛫 SVO ➔ Уфа | SU1424 | 24.08 16:30"
			icon := "🛫"
			fromTo := fmt.Sprintf("%s ➔ %s", strings.ToUpper(f.Provider), f.City)
			if f.Direction == "arr" {
				icon = "🛬"
				fromTo = fmt.Sprintf("%s ➔ %s", f.City, strings.ToUpper(f.Provider))
			}

			btnText := fmt.Sprintf("%s %s | %s | %s", icon, fromTo, f.Code, t.Format("02.01 15:04"))
			cbData := "sub:" + f.UID

			btn := tgbotapi.NewInlineKeyboardButtonData(btnText, cbData)
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
		}

		msgOut := tgbotapi.NewMessage(msg.Chat.ID, "🔎 *Найденные рейсы:*\nВыберите нужный для подписки:")
		msgOut.ParseMode = "Markdown"
		msgOut.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.api.Send(msgOut)

	default:
		b.send(chatID, fmt.Sprintf("❌ Неизвестная команда. Используйте [/help](%s) для списка доступных команд.", fmt.Sprintf(helpCommandURL, b.api.Self.UserName)))
	}
}

// Обработка нажатия на кнопку (выбор даты)
func (b *Bot) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	if strings.HasPrefix(cb.Data, "sub:") {
		uid := strings.TrimPrefix(cb.Data, "sub:")

		flight, err := b.repo.GetFlightByUID(ctx, uid)
		if err != nil {
			b.api.Request(tgbotapi.NewCallback(cb.ID, "Рейс не найден!"))
			return
		}

		_ = b.repo.SubscribeToUID(ctx, cb.Message.Chat.ID, flight.Code, uid)

		t, _ := time.Parse(time.RFC3339, flight.SchedTime)
		reply := fmt.Sprintf("✅ *Подписка оформлена!*\n\n📍 Направление: *%s*\n🕒 Вылет: *%s*\nℹ️ Статус: *%s*\n\n🔔 Вы будете получать уведомления в данном чате.",
			flight.City, t.Format("15:04 02.01"), flight.Status)

		editMsg := tgbotapi.NewEditMessageText(cb.Message.Chat.ID, cb.Message.MessageID, reply)
		editMsg.ParseMode = "Markdown"
		b.api.Send(editMsg)

		// Сообщаем телеграму, что колбек обработан
		b.api.Request(tgbotapi.NewCallback(cb.ID, "Подписка успешна!"))
	}
}

// SendAlert рассылает уведомления
func (b *Bot) SendAlert(ctx context.Context, newF, oldF model.Flight) {
	chats, _ := b.repo.GetSubscribersForUID(ctx, newF.UID)
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

// HandleAutoShift перебрасывает подписки на следующий день, если рейс завершился
func (b *Bot) HandleAutoShift(ctx context.Context, f model.Flight) {
	nextFlight, err := b.repo.GetNextFlight(ctx, f.Code, f.SchedTime)
	if err != nil || nextFlight == nil {
		return
	}

	chats, _ := b.repo.GetSubscribersForUID(ctx, f.UID)
	if len(chats) == 0 {
		return
	}

	_ = b.repo.UpdateSubscriptionUID(ctx, f.UID, nextFlight.UID)

	tNext, _ := time.Parse(time.RFC3339, nextFlight.SchedTime)
	msgText := fmt.Sprintf("🏁 Рейс *%s* завершен!\n\n♻️ Ваша подписка автоматически переключена на следующий рейс:\n📅 *%s* | ℹ️ %s",
		f.Code, tNext.Format("02.01 15:04"), nextFlight.Status) +
		fmt.Sprintf("\n🔗 [Отписаться](%s)", GetUnsubscribeURL(b.api.Self.UserName, f.Code))

	for _, chatID := range chats {
		b.send(chatID, msgText)
	}
}

func (b *Bot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, _ = b.api.Send(msg)
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
