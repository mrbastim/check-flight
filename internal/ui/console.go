package ui

import (
	"fmt"
	"time"

	"check-flight/internal/model"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorCyan   = "\033[36m"
	ColorBold   = "\033[1m"
)

func PrintAlert(newFlight, oldFlight model.Flight) {
	rows := make([][]string, 0, 2)

	if newFlight.Gate != oldFlight.Gate && newFlight.Gate != "" {
		oldG := oldFlight.Gate
		if oldG == "" {
			oldG = "Нет"
		}
		rows = append(rows, []string{"Выход на посадку", oldG, newFlight.Gate})
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
	fmt.Printf("%s│ ✈️  %-12s | 🕒 %-11s | 🌍 %-12s │%s\n", ColorCyan, newFlight.Code, displayTime, trimToWidth(newFlight.City, 12), ColorReset)
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
