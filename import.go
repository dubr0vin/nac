package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type Source struct {
	ID          string    `json:"id"`
	User        string    `json:"-" gorm:"index"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Timezone    string    `json:"timezone"`
	Tags        []string  `json:"tags" gorm:"serializer:json"`
	Interval    int       `json:"interval"`
	LastAttempt time.Time `json:"lastAttempt"`
	LastSuccess time.Time `json:"lastSuccess"`
	Error       string    `json:"error"`
	TokenHash   string    `json:"-"`
	HasToken    bool      `json:"hasToken" gorm:"-"`
}

func (a *App) source(id string) (Source, error) {
	var source Source
	err := a.db.First(&source, "id = ?", id).Error
	source.HasToken = source.TokenHash != ""
	return source, err
}

func (a *App) sources(user string) ([]Source, error) {
	sources := []Source{}
	query := a.db
	if user != "" {
		query = query.Where("user = ?", user)
	}
	err := query.Find(&sources).Error
	for i := range sources {
		sources[i].HasToken = sources[i].TokenHash != ""
	}
	return sources, err
}

func (a *App) sourceLock(id string) *sync.Mutex {
	lock, _ := a.sourceLocks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (a *App) putSource(c echo.Context) error {
	var source Source
	if err := c.Bind(&source); err != nil {
		return err
	}
	if strings.TrimSpace(source.Name) == "" {
		return echo.NewHTTPError(400, "name required")
	}
	if _, err := time.LoadLocation(source.Timezone); err != nil {
		return badRequest(err)
	}
	if source.Interval != 0 && (source.Interval < 60 || source.Interval > 31536000) {
		return echo.NewHTTPError(400, "interval is seconds; minimum 60, or 0 to disable")
	}
	if err := validateTags(source.Tags, false); err != nil {
		return badRequest(err)
	}
	if source.URL != "" {
		parsed, err := url.Parse(source.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return echo.NewHTTPError(400, "use an HTTP(S) URL without userinfo")
		}
	}
	source.ID = c.Param("id")
	if source.ID == "" {
		source.ID = newID()
	}
	lock := a.sourceLock(source.ID)
	lock.Lock()
	defer lock.Unlock()
	source.User = currentUser(c).ID
	if c.Request().Method == "PUT" {
		old, err := a.source(source.ID)
		if err != nil || old.User != source.User {
			return echo.ErrNotFound
		}
		source.TokenHash = old.TokenHash
		source.LastAttempt, source.LastSuccess, source.Error = old.LastAttempt, old.LastSuccess, old.Error
	} else {
		source.LastAttempt, source.LastSuccess, source.Error = time.Time{}, time.Time{}, ""
	}
	if err := a.db.Save(&source).Error; err != nil {
		return err
	}
	source.HasToken = source.TokenHash != ""
	return c.JSON(200, source)
}

func (a *App) deleteSource(c echo.Context) error {
	lock := a.sourceLock(c.Param("id"))
	lock.Lock()
	defer lock.Unlock()
	source, err := a.source(c.Param("id"))
	if err != nil || source.User != currentUser(c).ID {
		return echo.ErrNotFound
	}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Event{}).Where("source = ?", source.ID).Update("removed", true).Error; err != nil {
			return err
		}
		return tx.Delete(&source).Error
	})
	if err != nil {
		return err
	}
	return c.JSON(200, echo.Map{"ok": true})
}

func (a *App) sourceToken(c echo.Context) error {
	lock := a.sourceLock(c.Param("id"))
	lock.Lock()
	defer lock.Unlock()
	source, err := a.source(c.Param("id"))
	if err != nil || source.User != currentUser(c).ID {
		return echo.ErrNotFound
	}
	token, hash := "", ""
	if c.Request().Method == "POST" {
		token = newID() + newID()
		hash = digest(token)
	}
	if err := a.db.Model(&source).Update("token_hash", hash).Error; err != nil {
		return err
	}
	return c.JSON(200, echo.Map{"token": token})
}

func (a *App) refreshSource(c echo.Context) error {
	source, err := a.source(c.Param("id"))
	if err != nil || source.User != currentUser(c).ID {
		return echo.ErrNotFound
	}
	if err := a.pull(c.Request().Context(), source); err != nil {
		return badRequest(err)
	}
	return c.JSON(200, echo.Map{"ok": true})
}
func parseICS(reader io.Reader, s Source) (map[string]Event, error) {
	dec := ical.NewDecoder(reader)
	cal, err := dec.Decode()
	if err != nil {
		return nil, err
	}
	if _, err = dec.Decode(); err != io.EOF {
		return nil, errors.New("expected a single VCALENDAR")
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, err
	}
	out := map[string]Event{}
	overrides := map[string]map[string]*Event{}
	for _, c := range cal.Children {
		if c.Name == "VTIMEZONE" {
			continue
		}
		if c.Name != "VEVENT" {
			return nil, fmt.Errorf("unsupported component %s", c.Name)
		}
		uid, err := c.Props.Text("UID")
		if err != nil || uid == "" {
			return nil, errors.New("VEVENT requires UID")
		}
		if len(c.Props.Values("RRULE")) > 1 || c.Props.Get("EXRULE") != nil {
			return nil, errors.New("multiple RRULE or EXRULE is unsupported")
		}
		e := Event{UID: uid, Source: s.ID, Timezone: s.Timezone, Members: []Member{{User: s.User, Editor: true}}}
		readText := func(name string) (string, error) {
			if c.Props.Get(name) == nil {
				return "", nil
			}
			return c.Props.Text(name)
		}
		for prop, dst := range map[string]*string{"SUMMARY": &e.Title, "DESCRIPTION": &e.Description, "LOCATION": &e.Location} {
			*dst, err = readText(prop)
			if err != nil {
				return nil, err
			}
		}
		if p := c.Props.Get("URL"); p != nil {
			e.URL = p.Value
		}
		if p := c.Props.Get("RRULE"); p != nil {
			e.RRule = p.Value
		}
		if p := c.Props.Get("STATUS"); p != nil {
			e.Cancelled = p.Value == "CANCELLED"
		}
		p := c.Props.Get("DTSTART")
		if p == nil {
			return nil, errors.New("VEVENT requires DTSTART")
		}
		e.Start, err = p.DateTime(loc)
		if err != nil {
			return nil, err
		}
		e.AllDay = p.ValueType() == ical.ValueDate || len(p.Value) == 8
		e.Timezone = e.Start.Location().String()
		if end := c.Props.Get("DTEND"); end != nil {
			e.End, err = end.DateTime(loc)
		} else if dur := c.Props.Get("DURATION"); dur != nil {
			var d time.Duration
			d, err = dur.Duration()
			e.End = e.Start.Add(d)
		} else if e.AllDay {
			e.End = e.Start.AddDate(0, 0, 1)
		} else {
			e.End = e.Start.Add(time.Second)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range c.Props.Values("CATEGORIES") {
			values, err := p.TextList()
			if err != nil {
				return nil, err
			}
			for _, t := range values {
				e.Categories = append(e.Categories, "ics:"+t)
			}
		}
		for prop, dst := range map[string]*[]time.Time{"EXDATE": &e.ExDates, "RDATE": &e.RDates} {
			for _, p := range c.Props.Values(prop) {
				for _, v := range strings.Split(p.Value, ",") {
					p.Value = v
					t, err := p.DateTime(loc)
					if err != nil {
						return nil, err
					}
					*dst = append(*dst, t)
				}
			}
		}
		if err = e.Validate(); err != nil {
			return nil, fmt.Errorf("UID %s: %w", uid, err)
		}
		if p := c.Props.Get("RECURRENCE-ID"); p != nil {
			if p.Params.Get("RANGE") != "" {
				return nil, errors.New("RECURRENCE-ID RANGE is unsupported")
			}
			t, err := p.DateTime(loc)
			if err != nil {
				return nil, err
			}
			rid := recurrenceKey(t)
			if overrides[uid] == nil {
				overrides[uid] = map[string]*Event{}
			}
			if overrides[uid][rid] != nil {
				return nil, errors.New("duplicate recurrence override")
			}
			overrides[uid][rid] = &e
		} else {
			if _, exists := out[uid]; exists {
				return nil, errors.New("duplicate UID")
			}
			out[uid] = e
		}
		if len(out) > 10000 {
			return nil, errors.New("snapshot exceeds 10000 series")
		}
	}
	for uid, os := range overrides {
		e, ok := out[uid]
		if !ok {
			return nil, errors.New("recurrence override without master")
		}
		e.Overrides = os
		out[uid] = e
	}
	return out, nil
}

// The caller holds the source lock across fetching, parsing and applying.
func (a *App) importSnapshot(source Source, reader io.Reader) error {
	parsed, err := parseICS(reader, source)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var existing []Event
	if err := a.db.Where("source = ?", source.ID).Find(&existing).Error; err != nil {
		return err
	}
	byUID := map[string]Event{}
	for _, event := range existing {
		byUID[event.UID] = event
	}
	return a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Event{}).Where("source = ?", source.ID).Update("removed", true).Error; err != nil {
			return err
		}
		for uid, event := range parsed {
			old, found := byUID[uid]
			event.ID, event.Version = old.ID, old.Version+1
			if !found {
				event.ID = newID()
			}
			for _, override := range event.Overrides {
				override.ID, override.Version = event.ID, event.Version
			}
			if err := tx.Save(&event).Error; err != nil {
				return err
			}
			if !found {
				personal := Personal{EventID: event.ID, UserID: source.User, Tags: Tags{Add: source.Tags}}
				if err := tx.Create(&personal).Error; err != nil {
					return err
				}
			}
		}
		now := time.Now().UTC()
		return tx.Model(&source).Updates(map[string]any{
			"last_attempt": now, "last_success": now, "error": "",
		}).Error
	})
}

func (a *App) recordFailure(source Source, err error) {
	a.db.Model(&source).Updates(map[string]any{"last_attempt": time.Now().UTC(), "error": err.Error()})
}

func (a *App) pull(ctx context.Context, source Source) error {
	lock := a.sourceLock(source.ID)
	lock.Lock()
	defer lock.Unlock()
	source, err := a.source(source.ID)
	if err != nil {
		return err
	}
	err = a.fetchSnapshot(ctx, source)
	if err != nil {
		a.recordFailure(source, err)
	}
	return err
}

func (a *App) fetchSnapshot(ctx context.Context, source Source) error {
	if source.URL == "" {
		return errors.New("source has no pull URL")
	}
	request, err := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
	if err != nil {
		return errors.New("invalid pull URL")
	}
	request.Header.Set("Accept", "text/calendar")
	response, err := a.client.Do(request)
	if err != nil {
		return errors.New("cannot fetch source (connection or timeout)")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 16<<20 {
		return errors.New("ICS exceeds 16 MiB")
	}
	return a.importSnapshot(source, strings.NewReader(string(data)))
}

func (a *App) push(c echo.Context) error {
	lock := a.sourceLock(c.Param("id"))
	lock.Lock()
	defer lock.Unlock()
	source, err := a.source(c.Param("id"))
	if err != nil {
		return echo.ErrNotFound
	}
	request := c.Request()
	if auth := request.Header.Get("Authorization"); auth != "" {
		if !strings.HasPrefix(auth, "Bearer ") || source.TokenHash == "" || digest(strings.TrimPrefix(auth, "Bearer ")) != source.TokenHash {
			return echo.ErrUnauthorized
		}
	} else {
		if err := a.authenticate(c); err != nil {
			return err
		}
		if source.User != currentUser(c).ID {
			return echo.ErrNotFound
		}
		if request.Header.Get("X-NAC") != "1" {
			return echo.ErrForbidden
		}
	}
	if err := a.importSnapshot(source, request.Body); err != nil {
		a.recordFailure(source, err)
		return badRequest(err)
	}
	return c.JSON(200, echo.Map{"ok": true})
}

func (a *App) scheduler(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sources, err := a.sources("")
			if err != nil {
				continue
			}
			for _, source := range sources {
				if source.URL != "" && source.Interval > 0 && time.Since(source.LastAttempt) >= time.Duration(source.Interval)*time.Second {
					_ = a.pull(ctx, source)
				}
				if ctx.Err() != nil {
					return
				}
			}
		}
	}
}
