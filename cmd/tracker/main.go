package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"check-flight/internal/bot"
	"check-flight/internal/model"
	"check-flight/internal/monitor"
	"check-flight/internal/provider"
	"check-flight/internal/store"
	"check-flight/internal/ui"

	_ "github.com/mattn/go-sqlite3" // Драйвер SQLite
)

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func main() {
	logFile, err := os.OpenFile("tracker.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println(ui.ColorRed, "Не удалось открыть файл логов:", err, ui.ColorReset)
		os.Exit(1)
	}
	defer logFile.Close()

	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime)

	log.Println("=== ЗАПУСК ТРЕКЕРА ===")

	envProvider := getEnv("PROVIDER", "svo")
	envToken := getEnv("TELEGRAM_BOT_TOKEN", getEnv("TG_TOKEN", ""))

	// 2. Инициализируем флаги (дефолты берутся из ENV)
	providerParam := flag.String("provider", envProvider, "Провайдер/аэропорт (или env: PROVIDER)")
	tgToken := flag.String("token", envToken, "Telegram Bot Token (или env: TELEGRAM_BOT_TOKEN)")

	directionParam := flag.String("direction", "", "Направление рейсов (departure/arrival)")
	searchParam := flag.String("search", "", "Фильтр по направлению/городу")
	terminalParam := flag.String("terminal", "", "Фильтр по терминалу вылета")
	printParam := flag.Bool("no-printing", false, "Отключить вывод обновлений рейсов в консоль")

	flag.Parse()

	fmt.Println(ui.ColorCyan + ui.ColorBold + "✈️  Запуск трекера. Логи пишутся в tracker.log" + ui.ColorReset)

	p, err := provider.New(*providerParam)
	if err != nil {
		log.Println(err)
		fmt.Println(ui.ColorRed, err, ui.ColorReset)
		os.Exit(1)
	}
	providers := provider.NewRegistry(p)

	if *directionParam != "departure" && *directionParam != "arrival" && *directionParam != "" {
		log.Println("Не правильно задан параметр direction")
		fmt.Println(ui.ColorRed, "Не правильно задан параметр direction", ui.ColorReset)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := sql.Open("sqlite3", "./flights.db")
	if err != nil {
		log.Fatal("Ошибка открытия БД: ", err)
	}
	defer db.Close()

	sqlRepo := store.NewRepository(db)
	if err := sqlRepo.Init(db); err != nil {
		log.Fatal(err)
	}

	var tgBot *bot.Bot
	if *tgToken != "" {
		tgBot, err = bot.New(*tgToken, sqlRepo, providers)
		if err != nil {
			log.Fatalf("Ошибка создания бота: %v", err)
		}
		go tgBot.Start(ctx)
	} else {
		fmt.Println("⚠️ Токен Telegram не передан. Бот работает в режиме только консоли.")
	}

	query := model.Query{
		Direction: *directionParam,
		Search:    *searchParam,
		Terminal:  *terminalParam,
	}

	svc := monitor.NewService(sqlRepo, p, query, tgBot, !*printParam, 1*time.Minute)

	go svc.Start(ctx)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan
	cancel()

	log.Println("=== ТРЕКЕР ОСТАНОВЛЕН ===")
	fmt.Println(ui.ColorYellow + "\nТрекер корректно остановлен." + ui.ColorReset)
}
