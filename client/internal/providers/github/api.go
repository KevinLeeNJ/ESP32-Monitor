package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/platform/proxy"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const endpoint = "https://api.github.com/graphql"
const query = `query Calendar($login: String!, $from: DateTime!, $to: DateTime!) {
 user(login: $login) { login contributionsCollection(from: $from, to: $to) {
 contributionCalendar { weeks { contributionDays { date contributionCount contributionLevel } } }
 } }
 rateLimit { remaining resetAt }
}`

// Day keeps the upstream calendar date unchanged; it is not a UTC timestamp.
type Day struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Level int    `json:"level"`
}
type Calendar struct {
	Login string `json:"login"`
	Days  []Day  `json:"days"`
	Total int    `json:"total"`
	Today *int   `json:"today"`
}
type apiError struct {
	code  string
	retry time.Time
}

func (e *apiError) Error() string { return e.code }

type apiResponse struct {
	Data struct {
		User *struct {
			Login      string `json:"login"`
			Collection struct {
				Calendar struct {
					Weeks []struct {
						Days []struct {
							Date  string `json:"date"`
							Count *int   `json:"contributionCount"`
							Level string `json:"contributionLevel"`
						} `json:"contributionDays"`
					} `json:"weeks"`
				} `json:"contributionCalendar"`
			} `json:"contributionsCollection"`
		} `json:"user"`
		RateLimit struct {
			Remaining *int      `json:"remaining"`
			ResetAt   time.Time `json:"resetAt"`
		} `json:"rateLimit"`
	} `json:"data"`
	Errors []struct {
		Type string `json:"type"`
	} `json:"errors"`
}

func fetch(ctx context.Context, client *http.Client, url, token, login string, now time.Time) (Calendar, time.Time, error) {
	// An inclusive 365-day range fits GitHub's one-year collection limit and yields 52/53 weeks.
	end := now.UTC()
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -364)
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]string{"login": login, "from": start.Format(time.RFC3339), "to": end.Format(time.RFC3339)}})
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "esp32-monitor-client/0.1")
	resp, err := client.Do(req)
	if err != nil {
		for _, code := range []error{proxy.ErrAutomatic, proxy.ErrDetection, proxy.ErrInvalid} {
			if errors.Is(err, code) {
				return Calendar{}, time.Time{}, &apiError{code: code.Error()}
			}
		}
		return Calendar{}, time.Time{}, &apiError{code: "network_error"}
	}
	defer resp.Body.Close()
	retry := now.Add(time.Minute)
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
		retry = now.Add(time.Duration(min(secs, 604800)) * time.Second)
	} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && date.After(now) {
		retry = date
	}
	if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && time.Unix(reset, 0).After(retry) {
		retry = time.Unix(reset, 0)
	}
	if resp.StatusCode == 429 || (resp.StatusCode == 403 && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")) {
		return Calendar{}, time.Time{}, &apiError{"rate_limited", retry}
	}
	if resp.StatusCode == 401 {
		return Calendar{}, time.Time{}, &apiError{code: "auth_required"}
	}
	if resp.StatusCode == 403 {
		return Calendar{}, time.Time{}, &apiError{code: "permission_denied"}
	}
	if resp.StatusCode != 200 {
		return Calendar{}, time.Time{}, &apiError{code: "upstream_error"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return Calendar{}, time.Time{}, &apiError{code: "invalid_response"}
	}
	var wire apiResponse
	if json.Unmarshal(body, &wire) != nil {
		return Calendar{}, time.Time{}, &apiError{code: "invalid_response"}
	}
	for _, e := range wire.Errors {
		switch e.Type {
		case "RATE_LIMITED":
			return Calendar{}, time.Time{}, &apiError{"rate_limited", retry}
		case "FORBIDDEN":
			return Calendar{}, time.Time{}, &apiError{code: "permission_denied"}
		default:
			return Calendar{}, time.Time{}, &apiError{code: "graphql_error"}
		}
	}
	if wire.Data.User == nil {
		return Calendar{}, time.Time{}, &apiError{code: "user_not_found"}
	}
	days := map[string]Day{}
	levels := map[string]int{"NONE": 0, "FIRST_QUARTILE": 1, "SECOND_QUARTILE": 2, "THIRD_QUARTILE": 3, "FOURTH_QUARTILE": 4}
	for _, week := range wire.Data.User.Collection.Calendar.Weeks {
		for _, d := range week.Days {
			level, ok := levels[d.Level]
			if _, err := time.Parse("2006-01-02", d.Date); err != nil || d.Count == nil || *d.Count < 0 || !ok {
				return Calendar{}, time.Time{}, &apiError{code: "invalid_response"}
			}
			if d.Date < start.Format("2006-01-02") || d.Date > end.Format("2006-01-02") {
				continue
			}
			day := Day{d.Date, *d.Count, level}
			if previous, ok := days[d.Date]; ok && previous != day {
				return Calendar{}, time.Time{}, &apiError{code: "invalid_response"}
			}
			days[d.Date] = day
		}
	}
	if len(days) == 0 {
		return Calendar{}, time.Time{}, &apiError{code: "invalid_response"}
	}
	calendar := Calendar{Login: wire.Data.User.Login, Days: make([]Day, 0, len(days))}
	for _, d := range days {
		calendar.Days = append(calendar.Days, d)
		calendar.Total += d.Count
		if d.Date == end.Format("2006-01-02") {
			n := d.Count
			calendar.Today = &n
		}
	}
	sort.Slice(calendar.Days, func(i, j int) bool { return calendar.Days[i].Date < calendar.Days[j].Date })
	var next time.Time
	if wire.Data.RateLimit.Remaining != nil && *wire.Data.RateLimit.Remaining == 0 {
		next = wire.Data.RateLimit.ResetAt
	}
	return calendar, next, nil
}

func errorCode(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.code
	}
	return "internal_error"
}
