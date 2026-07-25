package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	historyCursorVersion  = 1
	historyDirectionOlder = "older"
	historyDirectionNewer = "newer"
	historyMaxWindowDays  = 366
)

type historyCursor struct {
	Version   int       `json:"v"`
	Direction string    `json:"direction"`
	At        time.Time `json:"at"`
	ID        uint      `json:"id"`
	Source    string    `json:"source,omitempty"`
}

type historyWindow struct {
	From time.Time
	To   time.Time
}

type historyKey struct {
	At     time.Time
	ID     uint
	Source string
}

func encodeHistoryCursor(key historyKey, direction string) (string, error) {
	if key.At.IsZero() || key.ID == 0 {
		return "", errors.New("history cursor requires a timestamp and id")
	}
	if direction != historyDirectionOlder && direction != historyDirectionNewer {
		return "", errors.New("invalid history cursor direction")
	}
	payload, err := json.Marshal(historyCursor{
		Version: historyCursorVersion, Direction: direction, At: key.At.UTC(), ID: key.ID, Source: key.Source,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeHistoryCursor(raw string, allowedSources map[string]struct{}) (*historyCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1024 {
		return nil, errors.New("cursor is too long")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid cursor encoding")
	}
	var cursor historyCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return nil, errors.New("invalid cursor payload")
	}
	if cursor.Version != historyCursorVersion || cursor.At.IsZero() || cursor.ID == 0 {
		return nil, errors.New("invalid cursor payload")
	}
	if cursor.Direction != historyDirectionOlder && cursor.Direction != historyDirectionNewer {
		return nil, errors.New("invalid cursor direction")
	}
	if allowedSources == nil {
		if cursor.Source != "" {
			return nil, errors.New("cursor source is not supported")
		}
	} else if _, ok := allowedSources[cursor.Source]; !ok {
		return nil, errors.New("invalid cursor source")
	}
	cursor.At = cursor.At.UTC()
	return &cursor, nil
}

func parseHistoryWindow(values url.Values, defaultDays int) (historyWindow, error) {
	return parseHistoryWindowAt(values, defaultDays, time.Now().UTC())
}

func parseHistoryWindowAt(values url.Values, defaultDays int, now time.Time) (historyWindow, error) {
	if defaultDays < 1 || defaultDays > historyMaxWindowDays {
		return historyWindow{}, errors.New("invalid default history window")
	}
	rawFrom := strings.TrimSpace(values.Get("from"))
	rawTo := strings.TrimSpace(values.Get("to"))
	defaultTo := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day()+1, 0, 0, 0, 0, time.UTC)
	window := historyWindow{}
	var err error
	if rawFrom != "" {
		window.From, err = parseHistoryBoundary(rawFrom, false)
		if err != nil {
			return historyWindow{}, fmt.Errorf("invalid from: %w", err)
		}
	}
	if rawTo != "" {
		window.To, err = parseHistoryBoundary(rawTo, true)
		if err != nil {
			return historyWindow{}, fmt.Errorf("invalid to: %w", err)
		}
	}
	switch {
	case window.From.IsZero() && window.To.IsZero():
		window.To = defaultTo
		window.From = window.To.AddDate(0, 0, -defaultDays)
	case window.From.IsZero():
		window.From = window.To.AddDate(0, 0, -defaultDays)
	case window.To.IsZero():
		window.To = window.From.AddDate(0, 0, defaultDays)
		if window.To.After(defaultTo) {
			window.To = defaultTo
		}
	}
	if !window.From.Before(window.To) {
		return historyWindow{}, errors.New("from must be before to")
	}
	if window.To.Sub(window.From) > historyMaxWindowDays*24*time.Hour {
		return historyWindow{}, fmt.Errorf("history window cannot exceed %d days", historyMaxWindowDays)
	}
	return window, nil
}

func parseOptionalDateWindow(values url.Values, fromKey, toKey string, maxDays int) (historyWindow, bool, error) {
	if maxDays < 1 || maxDays > historyMaxWindowDays {
		return historyWindow{}, false, errors.New("invalid optional date window")
	}
	rawFrom := strings.TrimSpace(values.Get(fromKey))
	rawTo := strings.TrimSpace(values.Get(toKey))
	if rawFrom == "" && rawTo == "" {
		return historyWindow{}, false, nil
	}
	if rawFrom == "" || rawTo == "" {
		return historyWindow{}, false, fmt.Errorf("%s and %s must be used together", fromKey, toKey)
	}
	from, err := parseHistoryBoundary(rawFrom, false)
	if err != nil {
		return historyWindow{}, false, fmt.Errorf("invalid %s: %w", fromKey, err)
	}
	to, err := parseHistoryBoundary(rawTo, true)
	if err != nil {
		return historyWindow{}, false, fmt.Errorf("invalid %s: %w", toKey, err)
	}
	if !from.Before(to) {
		return historyWindow{}, false, fmt.Errorf("%s must be before or equal to %s", fromKey, toKey)
	}
	if to.Sub(from) > time.Duration(maxDays)*24*time.Hour {
		return historyWindow{}, false, fmt.Errorf("date window cannot exceed %d days", maxDays)
	}
	return historyWindow{From: from, To: to}, true, nil
}

func parseHistoryBoundary(raw string, inclusiveDateEnd bool) (time.Time, error) {
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if inclusiveDateEnd {
			parsed = parsed.AddDate(0, 0, 1)
		}
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("use YYYY-MM-DD or RFC3339")
	}
	return parsed.UTC(), nil
}

func applyHistoryWindow(query *gorm.DB, column string, window historyWindow) *gorm.DB {
	return query.Where(column+" >= ? AND "+column+" < ?", window.From, window.To)
}

func applySimpleHistoryCursor(query *gorm.DB, column string, cursor *historyCursor) *gorm.DB {
	if cursor == nil {
		return query
	}
	if cursor.Direction == historyDirectionOlder {
		return query.Where("("+column+" < ?) OR ("+column+" = ? AND id < ?)", cursor.At, cursor.At, cursor.ID)
	}
	return query.Where("("+column+" > ?) OR ("+column+" = ? AND id > ?)", cursor.At, cursor.At, cursor.ID)
}

func simpleHistoryOrder(column string, cursor *historyCursor) string {
	if cursor != nil && cursor.Direction == historyDirectionNewer {
		return column + " asc, id asc"
	}
	return column + " desc, id desc"
}

func applyOperationHistoryCursor(query *gorm.DB, source string, cursor *historyCursor) *gorm.DB {
	if cursor == nil {
		return query
	}
	if cursor.Direction == historyDirectionOlder {
		switch strings.Compare(source, cursor.Source) {
		case -1:
			return query.Where("created_at < ?", cursor.At)
		case 0:
			return query.Where("(created_at < ?) OR (created_at = ? AND id < ?)", cursor.At, cursor.At, cursor.ID)
		default:
			return query.Where("created_at <= ?", cursor.At)
		}
	}
	switch strings.Compare(source, cursor.Source) {
	case -1:
		return query.Where("created_at >= ?", cursor.At)
	case 0:
		return query.Where("(created_at > ?) OR (created_at = ? AND id > ?)", cursor.At, cursor.At, cursor.ID)
	default:
		return query.Where("created_at > ?", cursor.At)
	}
}

func operationHistoryOrder(cursor *historyCursor) string {
	if cursor != nil && cursor.Direction == historyDirectionNewer {
		return "created_at asc, id asc"
	}
	return "created_at desc, id desc"
}

func reverseHistoryPage[T any](items []T) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func historyPageCursorValues(first, last historyKey, requested *historyCursor, hasMore bool) (*string, *string, error) {
	var nextCursor *string
	var previousCursor *string
	set := func(target **string, key historyKey, direction string) error {
		value, err := encodeHistoryCursor(key, direction)
		if err != nil {
			return err
		}
		*target = &value
		return nil
	}
	if requested == nil {
		if hasMore {
			if err := set(&nextCursor, last, historyDirectionOlder); err != nil {
				return nil, nil, err
			}
		}
		return nextCursor, previousCursor, nil
	}
	if requested.Direction == historyDirectionOlder {
		if err := set(&previousCursor, first, historyDirectionNewer); err != nil {
			return nil, nil, err
		}
		if hasMore {
			if err := set(&nextCursor, last, historyDirectionOlder); err != nil {
				return nil, nil, err
			}
		}
		return nextCursor, previousCursor, nil
	}
	if err := set(&nextCursor, last, historyDirectionOlder); err != nil {
		return nil, nil, err
	}
	if hasMore {
		if err := set(&previousCursor, first, historyDirectionNewer); err != nil {
			return nil, nil, err
		}
	}
	return nextCursor, previousCursor, nil
}

func cursorPagedData(items interface{}, total int64, limit int, nextCursor, previousCursor *string) map[string]interface{} {
	return map[string]interface{}{
		"items": items,
		"page": pageMetadata{
			Offset: 0, Limit: limit, Total: total, NextCursor: nextCursor, PreviousCursor: previousCursor,
		},
		"aggregates": map[string]interface{}{},
		"facets":     map[string]interface{}{},
		"total":      total,
		"offset":     0,
		"limit":      limit,
	}
}
