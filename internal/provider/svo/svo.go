package svo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"check-flight/internal/model"
)

type client struct{}

type flightAPI struct {
	AD          string `json:"ad"` // "D" (Вылет) или "A" (Прилет)
	Number      string `json:"flt"`
	IID         string `json:"i_id"`
	Status      string `json:"vip_status_rus"`
	Terminal    string `json:"term"`
	Gate        string `json:"gate_id"`
	Baggage     string `json:"bbel_id"`
	SchedTime   string `json:"t_st"`
	CheckInDesk string `json:"chin_id"`

	Company struct {
		Code string `json:"code"`
	} `json:"co"`

	// mar1 - пункт отправления
	Origin struct {
		City string `json:"city"`
	} `json:"mar1"`

	// mar2 - пункт назначения
	Destination struct {
		City string `json:"city"`
	} `json:"mar2"`
}

type response struct {
	Items []flightAPI `json:"items"`
}

const (
	timetable_bitrixURL = "https://www.svo.aero/bitrix/timetable/"
	timetable_ruURL     = "https://www.svo.aero/ru/timetable/"
)

func New() *client {
	return &client{}
}

func (c *client) ID() string {
	return "svo"
}

func (c *client) Name() string {
	return "Шереметьево"
}

func (c *client) GetFlightURL(internalID string, direction string) string {
	if direction == "arr" {
		return fmt.Sprintf("%sarrival/flight/%s/info", timetable_ruURL, internalID)
	}
	return fmt.Sprintf("%sdeparture/flight/%s/info", timetable_ruURL, internalID)
}

func (c *client) Fetch(ctx context.Context, query model.Query) ([]model.Flight, error) {
	msk := time.FixedZone("MSK", 3*60*60)
	now := time.Now().In(msk)

	dateStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, msk).Format(time.RFC3339)
	dateEnd := now.Add(48 * time.Hour).Format(time.RFC3339)

	params := url.Values{}
	params.Add("dateStart", dateStart)
	params.Add("dateEnd", dateEnd)
	params.Add("perPage", "99999")
	params.Add("page", "0")
	params.Add("locale", "ru")
	if query.Direction != "" {
		params.Add("direction", query.Direction)
	}
	if query.Search != "" {
		params.Add("search", query.Search)
	}
	if query.Terminal != "" {
		params.Add("terminal", query.Terminal)
	}

	fullURL := timetable_bitrixURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	req.Header.Set("Referer", "https://www.svo.aero/")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("svo api returned status %d", resp.StatusCode)
	}

	var data response
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	flights := make([]model.Flight, 0, len(data.Items))
	for _, item := range data.Items {
		if item.SchedTime == "" {
			continue
		}

		internalID := item.IID

		// Определяем реальное направление
		isArrival := strings.ToUpper(item.AD) == "A"
		direction := "dep"
		city := item.Destination.City

		if isArrival {
			direction = "arr"
			city = item.Origin.City
		}

		bbel := item.Baggage

		// Форматируем код и UID
		cleanCompany := strings.TrimSpace(item.Company.Code)
		cleanNum := strings.TrimSpace(item.Number)
		code := fmt.Sprintf("%s %s", cleanCompany, cleanNum)
		uid := fmt.Sprintf("%s:%s:%s%s:%s", c.ID(), direction, cleanCompany, cleanNum, item.SchedTime)

		flights = append(flights, model.Flight{
			UID:         uid,
			InternalID:  internalID,
			Provider:    c.ID(),
			Direction:   direction,
			Code:        code,
			City:        city,
			SchedTime:   item.SchedTime,
			Status:      item.Status,
			Gate:        item.Gate,
			Terminal:    item.Terminal,
			BaggageBelt: bbel,
			CheckInDesk: item.CheckInDesk,
		})
	}

	return flights, nil
}
