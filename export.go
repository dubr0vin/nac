package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/labstack/echo/v4"
	"go.yaml.in/yaml/v3"
)

type Export struct {
	Condition string   `json:"condition"`
	ID        string   `json:"id"`
	User      string   `json:"-" gorm:"index"`
	Name      string   `json:"name"`
	Rule      Rule     `json:"rule" gorm:"serializer:json"`
	Fields    []string `json:"fields" gorm:"serializer:json"`
	View      string   `json:"view"`
	Theme     string   `json:"theme"`
	Timezone  string   `json:"timezone"`
	Poll      int      `json:"poll"`
}

func (a *App) export(id string) (Export, error) {
	var item Export
	err := a.db.First(&item, "id = ?", id).Error
	if err == nil {
		err = item.fillCondition()
	}
	return item, err
}

func (a *App) exports(user string) ([]Export, error) {
	items := []Export{}
	err := a.db.Where("user = ?", user).Find(&items).Error
	if err == nil {
		for i := range items {
			if err = items[i].fillCondition(); err != nil {
				break
			}
		}
	}
	return items, err
}

func (item *Export) fillCondition() error {
	if item.Condition != "" {
		return nil
	}
	data, err := yaml.Marshal(item.Rule)
	item.Condition = string(data)
	return err
}

func (a *App) putExport(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var item Export
	if err := c.Bind(&item); err != nil {
		return err
	}
	if item.Condition != "" {
		if len(item.Condition) > 65536 {
			return badRequest(errors.New("config exceeds 64 KiB"))
		}
		decoder := yaml.NewDecoder(strings.NewReader(item.Condition))
		if err := decoder.Decode(&item.Rule); err != nil {
			return badRequest(err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return badRequest(errors.New("expected one YAML document"))
		}
	}
	if item.Name == "" {
		return echo.NewHTTPError(400, "name required")
	}
	if err := item.Rule.Validate(0); err != nil {
		return badRequest(err)
	}
	for _, field := range item.Fields {
		if !slices.Contains([]string{"title", "description", "location", "url", "members", "tags"}, field) {
			return echo.NewHTTPError(400, "unknown export field")
		}
	}
	if err := item.validateView(); err != nil {
		return err
	}
	item.ID, item.User = c.Param("id"), currentUser(c).ID
	if item.ID == "" {
		item.ID = newID()
	} else {
		old, err := a.export(item.ID)
		if err != nil || old.User != item.User {
			return echo.ErrNotFound
		}
	}
	if err := a.db.Save(&item).Error; err != nil {
		return err
	}
	return c.JSON(200, item)
}

func (item Export) validateView() error {
	if !slices.Contains([]string{"day", "week", "month", "year", "list"}, item.View) ||
		!slices.Contains([]string{"light", "dark", "auto"}, item.Theme) || item.Poll < 5 || item.Poll > 3600 {
		return echo.NewHTTPError(400, "invalid view, theme or polling interval (5–3600 seconds)")
	}
	if _, err := time.LoadLocation(item.Timezone); err != nil {
		return badRequest(err)
	}
	return nil
}

func (a *App) putKiosk(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	item, err := a.export(c.Param("id"))
	if err != nil || item.User != currentUser(c).ID {
		return echo.ErrNotFound
	}
	var input Export
	if err := c.Bind(&input); err != nil {
		return err
	}
	if err := input.validateView(); err != nil {
		return err
	}
	item.View, item.Theme, item.Timezone, item.Poll = input.View, input.Theme, input.Timezone, input.Poll
	if err := a.db.Model(&item).Select("View", "Theme", "Timezone", "Poll").Updates(item).Error; err != nil {
		return err
	}
	return c.JSON(200, item)
}

func (a *App) deleteExport(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := a.db.Where("id = ? AND user = ?", c.Param("id"), currentUser(c).ID).Delete(&Export{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return echo.ErrNotFound
	}
	return c.JSON(200, echo.Map{"ok": true})
}

func (a *App) rotateExport(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	item, err := a.export(c.Param("id"))
	if err != nil || item.User != currentUser(c).ID {
		return echo.ErrNotFound
	}
	id := newID()
	if err := a.db.Model(&item).Update("id", id).Error; err != nil {
		return err
	}
	item.ID = id
	return c.JSON(200, item)
}

func (a *App) viewData(c echo.Context) error {
	item, err := a.export(c.QueryParam("id"))
	if err != nil {
		return echo.ErrNotFound
	}
	owner, err := a.user(item.User)
	if err != nil {
		return err
	}
	from, to, err := window(c)
	if err != nil {
		return err
	}
	events, err := a.events(owner.ID)
	if err != nil {
		return err
	}
	result := []Occurrence{}
	for _, event := range events {
		tags, err := a.tagState(event.ID, owner.ID)
		if err != nil {
			return err
		}
		instances, err := expand(event, from, to)
		if err != nil {
			return echo.NewHTTPError(422, err.Error())
		}
		for rid, instance := range instances {
			personal := tagsFor(event, rid, tags)
			instance.Members = event.Members
			if !instance.Cancelled && item.Rule.Match(personal) {
				result = append(result, a.project(item, instance, personal, publicID(item.ID, event.ID, rid), owner.Settings.EventColor(personal)))
			}
		}
	}
	slices.SortFunc(result, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
	// The rule tree itself contains private tag names, so it stays on the server.
	return c.JSON(200, echo.Map{
		"name": item.Name, "view": item.View, "theme": item.Theme,
		"timezone": item.Timezone, "poll": item.Poll, "events": result,
	})
}

func (a *App) exportICS(c echo.Context) error {
	item, err := a.export(c.QueryParam("id"))
	if err != nil {
		return echo.ErrNotFound
	}
	data, err := a.calendar(item)
	if err != nil {
		return err
	}
	return c.Blob(200, "text/calendar; charset=utf-8", data)
}

// This is the only projection of private events used by public endpoints.
func (a *App) project(item Export, event Event, tags []string, id string, color ColorRule) Occurrence {
	result := Occurrence{
		ID: id, Start: event.Start, End: event.End,
		AllDay: event.AllDay, Timezone: event.Timezone, Color: color.Color, Stripe: color.Stripe,
	}
	for _, field := range item.Fields {
		switch field {
		case "title":
			result.Title = event.Title
		case "description":
			result.Description = event.Description
		case "location":
			result.Location = event.Location
		case "url":
			result.URL = event.URL
		case "members":
			for _, member := range event.Members {
				user, err := a.user(member.User)
				if err == nil {
					result.Participants = append(result.Participants, Participant{
						ID: publicID(item.ID, user.ID, "user"), Name: user.Name,
					})
				}
			}
		case "tags":
			result.Tags = tags
		}
	}
	return result
}

func setCalendarTime(props ical.Props, name string, value time.Time, allDay bool, timezone string) {
	location, _ := time.LoadLocation(timezone)
	if allDay {
		props.SetDate(name, value.In(location))
	} else {
		props.SetDateTime(name, value.In(location))
	}
}

func (a *App) exportComponent(item Export, owner User, event Event, tags []string, uid, rid string) *ical.Component {
	visible := a.project(item, event, tags, uid, ColorRule{})
	component := ical.NewComponent("VEVENT")
	props := component.Props
	props.SetText("UID", uid)
	props.SetDateTime("DTSTAMP", time.Now().UTC())
	setCalendarTime(props, "DTSTART", visible.Start, visible.AllDay, event.Timezone)
	setCalendarTime(props, "DTEND", visible.End, visible.AllDay, event.Timezone)
	if rid != "" {
		original, _ := time.Parse(time.RFC3339, rid)
		setCalendarTime(props, "RECURRENCE-ID", original, event.AllDay, event.Timezone)
	}
	for field, value := range map[string]string{
		"SUMMARY": visible.Title, "DESCRIPTION": visible.Description,
		"LOCATION": visible.Location,
	} {
		if value != "" {
			props.SetText(field, value)
		}
	}
	if visible.URL != "" {
		props.Set(&ical.Prop{Name: "URL", Value: visible.URL})
	}
	if len(visible.Tags) > 0 {
		property := ical.NewProp("CATEGORIES")
		property.SetTextList(visible.Tags)
		props.Set(property)
	}
	for _, participant := range visible.Participants {
		property := ical.NewProp("ATTENDEE")
		property.Value = "urn:uuid:" + participant.ID
		property.Params.Set("CN", participant.Name)
		props.Add(property)
	}
	return component
}

func (a *App) calendar(item Export) ([]byte, error) {
	owner, err := a.user(item.User)
	if err != nil {
		return nil, err
	}
	events, err := a.events(owner.ID)
	if err != nil {
		return nil, err
	}
	calendar := ical.NewCalendar()
	calendar.Props.SetText("VERSION", "2.0")
	calendar.Props.SetText("PRODID", "-//NAC//Calendar//EN")
	for _, event := range events {
		state, err := a.tagState(event.ID, owner.ID)
		if err != nil {
			return nil, err
		}
		baseTags := tagsFor(event, "", state)
		baseVisible := !event.Cancelled && item.Rule.Match(baseTags)
		uid := publicID(item.ID, event.ID, "")
		var master *ical.Component
		if baseVisible {
			master = a.exportComponent(item, owner, event, baseTags, uid, "")
			if event.RRule != "" {
				master.Props.Set(&ical.Prop{Name: "RRULE", Value: event.RRule})
			}
			for name, dates := range map[string][]time.Time{"RDATE": event.RDates, "EXDATE": event.ExDates} {
				for _, date := range dates {
					props := make(ical.Props)
					setCalendarTime(props, name, date, event.AllDay, event.Timezone)
					master.Props.Add(props.Get(name))
				}
			}
			calendar.Children = append(calendar.Children, master)
		}
		// Both data overrides and personal tag overrides can change export membership.
		ids := map[string]bool{}
		for rid := range event.Overrides {
			ids[rid] = true
		}
		for rid := range state {
			if rid != "" {
				ids[rid] = true
			}
		}
		for rid := range ids {
			date, err := time.Parse(time.RFC3339, rid)
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(event.ExDates, func(excluded time.Time) bool { return excluded.Equal(date) }) {
				continue
			}
			// Personal tags may outlive an old recurrence rule. Do not invent occurrences.
			occurrences, err := expand(event, date.Add(-time.Second), date.Add(time.Second))
			if err != nil {
				return nil, err
			}
			if _, exists := occurrences[rid]; !exists && event.Overrides[rid] == nil {
				continue
			}
			occurrence := instance(event, date)
			occurrence.Members = event.Members
			tags := tagsFor(event, rid, state)
			visible := !occurrence.Cancelled && item.Rule.Match(tags)
			if baseVisible && !visible {
				props := make(ical.Props)
				setCalendarTime(props, "EXDATE", date, event.AllDay, event.Timezone)
				master.Props.Add(props.Get("EXDATE"))
			} else if visible {
				instanceUID, recurrenceID := uid, rid
				if !baseVisible {
					instanceUID = publicID(item.ID, event.ID, rid)
					recurrenceID = ""
				}
				component := a.exportComponent(item, owner, occurrence, tags, instanceUID, "")
				if recurrenceID != "" {
					setCalendarTime(component.Props, "RECURRENCE-ID", date, event.AllDay, event.Timezone)
				}
				calendar.Children = append(calendar.Children, component)
			}
		}
	}
	// go-ical rejects empty calendars; an empty feed is necessary for snapshot consumers.
	if len(calendar.Children) == 0 {
		return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//NAC//Calendar//EN\r\nEND:VCALENDAR\r\n"), nil
	}
	var buffer bytes.Buffer
	if err := ical.NewEncoder(&buffer).Encode(calendar); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
