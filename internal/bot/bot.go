package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"check-flight/internal/model"
	"check-flight/internal/store"
)

type Bot struct {
	api  *tgbotapi.BotAPI
	repo *store.Repository
}

func New(token string, db *sql.DB) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram auth error: %w", err)
	}

	log.Printf("Авторизован Telegram-бот: @%s", api.Self.UserName)
	repo := store.NewRepository(db)
	return &Bot{api: api, repo: repo}, nil
}

// Start запускает бесконечный цикл обработки команд
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-updates:
			if update.Message == nil || !update.Message.IsCommand() {
				continue
			}

			b.handleCommand(update.Message)
		}
	}
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	args := strings.TrimSpace(msg.CommandArguments())

	switch msg.Command() {
	case "start", "help":
		text := "✈️ *Бот отслеживания авиарейсов*\n\n" +
			"Команды:\n" +
			"• `/track SU 1484` — подписаться на уведомления по рейсу\n" +
			"• `/untrack SU 1484` — отписаться от рейса\n" +
			"• `/list` — мои активные подписки"
		b.send(chatID, text)

	case "track":
		if args == "" {
			b.send(chatID, "❌ Укажите номер рейса. Пример: `/track SU 1484`")
			return
		}

		code := store.NormalizeFlightCode(args)
		if err := b.repo.AddSubscription(context.Background(), chatID, code); err != nil {
			b.send(chatID, "❌ Ошибка сохранения подписки")
			return
		}

		// есть ли уже данные по этому рейсу в базе
		flight, err := b.repo.FindLatestFlight(context.Background(), code)
		if err == nil && flight != nil {
			t, _ := time.Parse(time.RFC3339, flight.SchedTime)
			gateStr := flight.Gate
			if gateStr == "" {
				gateStr = "Не назначен"
			}

			reply := fmt.Sprintf("✅ *Подписка оформлена на %s*!\n\n"+
				"📍 Направление: *%s*\n"+
				"🕒 Вылет: *%s*\n"+
				"🚪 Гейт: *%s* (Терминал %s)\n"+
				"ℹ️ Статус: *%s*\n\n"+
				"🔔 Я пришлю сообщение, как только изменится гейт или статус.",
				flight.Code, flight.City, t.Format("02.01 15:04"), gateStr, flight.Terminal, flight.Status)
			b.send(chatID, reply)
		} else {
			b.send(chatID, fmt.Sprintf("✅ Подписка на *%s* сохранена! Мы пришлем уведомление, когда рейс появится в табло.", code))
		}

	case "untrack":
		if args == "" {
			b.send(chatID, "❌ Укажите номер рейса. Пример: `/untrack SU 1484`")
			return
		}
		code := store.NormalizeFlightCode(args)
		_ = b.repo.RemoveSubscription(context.Background(), chatID, code)
		b.send(chatID, fmt.Sprintf("🗑 Вы отписались от уведомлений по рейсу *%s*", code))

	case "list":
		subs, err := b.repo.GetSubscriptions(context.Background(), chatID)
		if err != nil || len(subs) == 0 {
			b.send(chatID, "📭 У вас пока нет активных подписок.\nДобавьте: `/track SU 1484`")
			return
		}

		var sb strings.Builder
		sb.WriteString("📋 *Ваши подписки:*\n\n")
		for _, s := range subs {
			sb.WriteString(fmt.Sprintf("• `%s`\n", s))
		}
		b.send(chatID, sb.String())
	}
}

// SendAlert рассылает уведомление всем подписчикам конкретного рейса
func (b *Bot) SendAlert(newFlight, oldFlight model.Flight) {
	chats, err := b.repo.GetSubscribersForFlight(context.Background(), newFlight.Code)
	if err != nil || len(chats) == 0 {
		return
	}

	t, _ := time.Parse(time.RFC3339, newFlight.SchedTime)
	timeStr := t.Format("02.01 15:04")

	var changes []string
	if newFlight.Gate != oldFlight.Gate && newFlight.Gate != "" {
		oldG := oldFlight.Gate
		if oldG == "" {
			oldG = "Нет"
		}
		changes = append(changes, fmt.Sprintf("🚪 *Гейт:* %s ➔ *%s*", oldG, newFlight.Gate))
	}

	if newFlight.Status != oldFlight.Status && newFlight.Status != "" {
		oldS := oldFlight.Status
		if oldS == "" {
			oldS = "По расписанию"
		}
		changes = append(changes, fmt.Sprintf("ℹ️ *Статус:* %s ➔ *%s*", oldS, newFlight.Status))
	}

	if len(changes) == 0 {
		return
	}

	msgText := fmt.Sprintf("🔔 *ИЗМЕНЕНИЕ ПО РЕЙСУ %s*\n"+
		"🌍 Направление: *%s*\n"+
		"🕒 Время: *%s*\n\n"+
		"%s",
		newFlight.Code, newFlight.City, timeStr, strings.Join(changes, "\n"))

	for _, chatID := range chats {
		b.send(chatID, msgText)
	}
}

func (b *Bot) send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	_, _ = b.api.Send(msg)
}
