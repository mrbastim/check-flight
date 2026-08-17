package svo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"check-flight/internal/model"
)

type client struct{}

type flightAPI struct {
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

type response struct {
	Items []flightAPI `json:"items"`
}

func New() *client {
	return &client{}
}

func (c *client) ID() string {
	return "svo"
}

func (c *client) Fetch(ctx context.Context, query model.Query) ([]model.Flight, error) {
	msk := time.FixedZone("MSK", 3*60*60)
	now := time.Now().In(msk)

	dateStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, msk).Format(time.RFC3339)
	dateEnd := now.Add(48 * time.Hour).Format(time.RFC3339)

	params := url.Values{}
	params.Add("direction", query.Direction)
	params.Add("dateStart", dateStart)
	params.Add("dateEnd", dateEnd)
	params.Add("perPage", "99999")
	params.Add("page", "0")
	params.Add("locale", "ru")

	if query.Search != "" {
		params.Add("search", query.Search)
	}
	if query.Terminal != "" {
		params.Add("terminal", query.Terminal)
	}

	fullURL := "https://www.svo.aero/bitrix/timetable/?" + params.Encode()
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

		code := fmt.Sprintf("%s %s", item.Company.Code, item.Number)
		uid := fmt.Sprintf("%s_%s", code, item.SchedTime)

		flights = append(flights, model.Flight{
			UID:         uid,
			Code:        code,
			Destination: item.Destination.City,
			SchedTime:   item.SchedTime,
			Status:      item.Status,
			Gate:        item.Gate,
			Terminal:    item.Terminal,
		})
	}

	return flights, nil
}
