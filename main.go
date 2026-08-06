package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3" // Драйвер SQLite
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

type Flight struct {
	Number    string `json:"flt"`
	Status    string `json:"vip_status_rus"`
	Terminal  string `json:"term"`
	Gate      string `json:"gate_id"`
	SchedTime string `json:"t_st"`

	Company struct {
		Code string `json:"code"`
	} `json:"co"`

	Destination struct {
		City string `json:"city"`
	} `json:"mar2"`
}

type SvoResponse struct {
	Items []Flight `json:"items"`
}

func main() {
	// 1. НАСТРОЙКА ЛОГГЕРА В ФАЙЛ
	logFile, err := os.OpenFile("tracker.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println(ColorRed, "Не удалось открыть файл логов:", err, ColorReset)
		os.Exit(1)
	}
	defer logFile.Close()

	// Настраиваем стандартный логгер: пишем в файл, добавляем дату и время
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime)

	log.Println("=== ЗАПУСК ТРЕКЕРА ===")

	fmt.Println(ColorCyan + ColorBold + "✈️  Запуск трекера Шереметьево. Логи пишутся в tracker.log" + ColorReset)

	searchParam := flag.String("search", "", "Фильтр по направлению/городу")
	terminalParam := flag.String("terminal", "", "Фильтр по терминалу вылета")
	flag.Parse()

	db, err := sql.Open("sqlite3", "./flights.db")
	if err != nil {
		log.Fatal("КРИТИЧЕСКАЯ ОШИБКА: Ошибка открытия БД: ", err)
	}
	defer db.Close()

	initDB(db)

	go startMonitoring(db, *searchParam, *terminalParam)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan

	log.Println("=== ТРЕКЕР ОСТАНОВЛЕН ===")
	fmt.Println(ColorYellow + "\n🛑 Трекер корректно остановлен." + ColorReset)
}

func initDB(db *sql.DB) {
	query := `
	CREATE TABLE IF NOT EXISTS flights (
		uid TEXT PRIMARY KEY,
		flight_code TEXT,
		destination TEXT,
		sched_time DATETIME,
		status TEXT,
		gate TEXT,
		terminal TEXT,
		updated_at DATETIME
	);`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal("КРИТИЧЕСКАЯ ОШИБКА: Ошибка создания таблицы: ", err)
	}
}

func startMonitoring(db *sql.DB, searchParam, terminalParam string) {
	log.Printf("Параметры запуска -> Поиск: '%s', Терминал: '%s'\n", searchParam, terminalParam)

	processFlights(db, searchParam, terminalParam)

	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		processFlights(db, searchParam, terminalParam)
	}
}

func processFlights(db *sql.DB, searchParam, terminalParam string) {
	log.Println("Начат опрос API Шереметьево...")

	flights, err := fetchSVO("departure", searchParam, terminalParam)
	if err != nil {
		log.Printf("ОШИБКА API: %v\n", err)
		return
	}

	log.Printf("Успешно получено рейсов: %d\n", len(flights))

	savedFlights := loadFlightsFromDB(db)

	tx, err := db.Begin()
	if err != nil {
		log.Println("ОШИБКА БД (Begin Tx):", err)
		return
	}

	changesCount := 0

	for _, apiFlight := range flights {
		if apiFlight.SchedTime == "" {
			continue
		}

		flightCode := fmt.Sprintf("%s %s", apiFlight.Company.Code, apiFlight.Number)
		uid := fmt.Sprintf("%s_%s", flightCode, apiFlight.SchedTime)

		dbFlight, exists := savedFlights[uid]

		if exists {
			statusChanged := apiFlight.Status != "" && apiFlight.Status != dbFlight.Status
			gateChanged := apiFlight.Gate != "" && apiFlight.Gate != dbFlight.Gate

			if statusChanged || gateChanged {
				changesCount++

				// 1. Красиво выводим в консоль
				printAlert(flightCode, apiFlight.Destination.City, dbFlight, apiFlight)

				// 2. Пишем технический лог в файл (в одну строку, без кракозябр цветов)
				log.Printf("АЛЕРТ [%s]: Рейс %s (%s). Статус: '%s' -> '%s' | Гейт: '%s' -> '%s'",
					uid, flightCode, apiFlight.Destination.City,
					dbFlight.Status, apiFlight.Status,
					dbFlight.Gate, apiFlight.Gate)

				updateQuery := `UPDATE flights SET status=?, gate=?, terminal=?, updated_at=CURRENT_TIMESTAMP WHERE uid=?`
				_, err = tx.Exec(updateQuery, apiFlight.Status, apiFlight.Gate, apiFlight.Terminal, uid)
				if err != nil {
					log.Printf("ОШИБКА БД (Update %s): %v\n", uid, err)
				}
			}
		} else {
			insertQuery := `INSERT INTO flights (uid, flight_code, destination, sched_time, status, gate, terminal, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`
			_, err = tx.Exec(insertQuery, uid, flightCode, apiFlight.Destination.City, apiFlight.SchedTime, apiFlight.Status, apiFlight.Gate, apiFlight.Terminal)
			if err != nil {
				log.Printf("ОШИБКА БД (Insert %s): %v\n", uid, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		log.Println("ОШИБКА БД (Commit Tx):", err)
	}

	log.Printf("Опрос завершен. Найдено изменений: %d\n", changesCount)
}

func printAlert(flightCode, city string, oldFlight, newFlight Flight) {
	rows := make([][]string, 0, 2)

	if newFlight.Gate != oldFlight.Gate && newFlight.Gate != "" {
		oldG := oldFlight.Gate
		if oldG == "" {
			oldG = "Нет"
		}
		rows = append(rows, []string{"Гейт", oldG, newFlight.Gate})
	}

	if newFlight.Status != oldFlight.Status && newFlight.Status != "" {
		oldS := oldFlight.Status
		if oldS == "" {
			oldS = "По расписанию"
		}
		rows = append(rows, []string{"Статус", oldS, newFlight.Status})
	}

	if len(rows) == 0 {
		return
	}

	displayTime := newFlight.SchedTime
	t, err := time.Parse(time.RFC3339, newFlight.SchedTime)
	if err == nil {
		displayTime = t.Format("02.01 15:04")
	}

	fmt.Printf("\n%s╭────────────────────────────────────────────────────────────╮%s\n", ColorCyan, ColorReset)
	fmt.Printf("%s│ ✈️  %-12s | 🕒 %-11s | 🌍 %-12s │%s\n", ColorCyan, flightCode, displayTime, trimToWidth(city, 12), ColorReset)
	fmt.Printf("%s├──────────────┬────────────────────────┬────────────────────────┤%s\n", ColorCyan, ColorReset)
	fmt.Printf("%s│ %-12s │ %-22s │ %-22s │%s\n", ColorCyan, "Изменение", "Было", "Стало", ColorReset)
	fmt.Printf("%s├──────────────┼────────────────────────┼────────────────────────┤%s\n", ColorCyan, ColorReset)

	for _, row := range rows {
		fmt.Printf("%s│ %-12s │ %-22s │ %-22s │%s\n", ColorCyan, row[0], trimToWidth(row[1], 22), trimToWidth(row[2], 22), ColorReset)
	}
	fmt.Printf("%s╰──────────────┴────────────────────────┴────────────────────────╯%s\n", ColorCyan, ColorReset)
}

func trimToWidth(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func loadFlightsFromDB(db *sql.DB) map[string]Flight {
	rows, err := db.Query("SELECT uid, status, gate, terminal, sched_time FROM flights")
	if err != nil {
		log.Println("ОШИБКА БД (Select):", err)
		return make(map[string]Flight)
	}
	defer rows.Close()

	res := make(map[string]Flight)
	for rows.Next() {
		var f Flight
		var uid string
		if err := rows.Scan(&uid, &f.Status, &f.Gate, &f.Terminal, &f.SchedTime); err == nil {
			res[uid] = f
		}
	}
	if err := rows.Err(); err != nil {
		log.Println("ОШИБКА БД (Rows Err):", err)
	}
	return res
}

func fetchSVO(direction, search, terminal string) ([]Flight, error) {
	msk := time.FixedZone("MSK", 3*60*60)
	now := time.Now().In(msk)

	dateStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, msk).Format(time.RFC3339)
	dateEnd := now.Add(48 * time.Hour).Format(time.RFC3339)

	params := url.Values{}
	params.Add("direction", direction)
	params.Add("dateStart", dateStart)
	params.Add("dateEnd", dateEnd)
	params.Add("perPage", "99999")
	params.Add("page", "0")
	params.Add("locale", "ru")

	if search != "" {
		params.Add("search", search)
	}
	if terminal != "" {
		params.Add("terminal", terminal)
	}

	fullURL := "https://www.svo.aero/bitrix/timetable/?" + params.Encode()
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.Header.Set("Referer", "https://www.svo.aero/")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data SvoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return data.Items, nil
}
