package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"check-flight/internal/bot"
	"check-flight/internal/model"
	"check-flight/internal/monitor"
	"check-flight/internal/provider"
	"check-flight/internal/store"
	"check-flight/internal/ui"
)

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func main() {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Println(ui.ColorRed, "Не удалось создать каталог логов:", err, ui.ColorReset)
		os.Exit(1)
	}

	logPath := filepath.Join(logDir, "tracker-"+time.Now().Format("20060102-150405")+".log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println(ui.ColorRed, "Не удалось открыть файл логов:", err, ui.ColorReset)
		os.Exit(1)
	}
	defer logFile.Close()

	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.Ldate | log.Ltime)

	log.Println("=== ЗАПУСК ТРЕКЕРА ===")

	envProvider := getEnv("PROVIDER", "svo")
	envToken := getEnv("TELEGRAM_BOT_TOKEN", getEnv("TG_TOKEN", ""))
	envDBDriver := getEnv("DB_DRIVER", "sqlite")
	envDatabaseURL := getEnv("DATABASE_URL", "./flights.db")

	providerParam := flag.String("provider", envProvider, "Провайдер/аэропорт (или env: PROVIDER)")
	tgToken := flag.String("token", envToken, "Telegram Bot Token (или env: TELEGRAM_BOT_TOKEN)")

	directionParam := flag.String("direction", "", "Направление рейсов (departure/arrival)")
	searchParam := flag.String("search", "", "Фильтр по направлению/городу")
	terminalParam := flag.String("terminal", "", "Фильтр по терминалу вылета")
	printParam := flag.Bool("no-printing", false, "Отключить вывод обновлений рейсов в консоль")

	flag.Parse()

	fmt.Println(ui.ColorCyan + ui.ColorBold + "✈️  Запуск трекера. Лог: " + logPath + ui.ColorReset)

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

	db, repository, err := store.Open(envDBDriver, envDatabaseURL)
	if err != nil {
		log.Fatal("Ошибка открытия БД: ", err)
	}
	defer db.Close()

	var tgBot *bot.Bot
	if *tgToken != "" {
		tgBot, err = bot.New(*tgToken, repository, providers)
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

	svc := monitor.NewService(repository, p, query, tgBot, !*printParam, 1*time.Minute)

	go svc.Start(ctx)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan
	cancel()

	log.Println("=== ТРЕКЕР ОСТАНОВЛЕН ===")
	fmt.Println(ui.ColorYellow + "\nТрекер корректно остановлен." + ui.ColorReset)
}
