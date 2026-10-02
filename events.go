package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (a *App) getEvent(c echo.Context) error {
	user := currentUser(c)
	event, err := a.event(c.Param("id"))
	if err != nil || !event.IsMember(user.ID) {
		return echo.ErrNotFound
	}
	tags, err := a.tagState(event.ID, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(200, echo.Map{"event": event, "tags": tags})
}

func (a *App) listEvents(c echo.Context) error {
	viewer := currentUser(c)
	from, to, err := window(c)
	if err != nil {
		return err
	}
	target := c.QueryParam("user")
	if target == "" {
		target = viewer.ID
	}
	result, err := a.visibleEvents(viewer, target, from, to)
	if err != nil {
		return err
	}
	return c.JSON(200, result)
}

func (a *App) availability(c echo.Context) error {
	from, to, err := window(c)
	if err != nil {
		return err
	}
	var users []string
	if err := c.Bind(&users); err != nil {
		return err
	}
	if len(users) > 1000 {
		return echo.NewHTTPError(400, "too many participants")
	}
	result := map[string][]Occurrence{}
	for _, id := range unique(users) {
		events, err := a.visibleEvents(currentUser(c), id, from, to)
		if err != nil {
			return err
		}
		result[id] = events
	}
	return c.JSON(200, result)
}

func (a *App) visibleEvents(viewer User, target string, from, to time.Time) ([]Occurrence, error) {
	owner, err := a.user(target)
	if err != nil {
		return nil, echo.ErrNotFound
	}
	events, err := a.events(target)
	if err != nil {
		return nil, err
	}
	result := []Occurrence{}
	for _, event := range events {
		state, err := a.tagState(event.ID, target)
		if err != nil {
			return nil, err
		}
		ownState := state
		if target != viewer.ID && event.IsMember(viewer.ID) {
			ownState, err = a.tagState(event.ID, viewer.ID)
			if err != nil {
				return nil, err
			}
		}
		instances, err := expand(event, from, to)
		if err != nil {
			return nil, echo.NewHTTPError(422, err.Error())
		}
		for rid, instance := range instances {
			tags := tagsFor(event, rid, state)
			busy := !instance.Cancelled && owner.Settings.Busy.Match(tags)
			if !event.IsMember(viewer.ID) {
				if busy {
					result = append(result, Occurrence{
						ID:    publicID(viewer.ID, event.ID, rid),
						Start: instance.Start, End: instance.End, AllDay: instance.AllDay,
						Timezone: instance.Timezone, Color: "#868e96", Busy: true,
					})
				}
				continue
			}
			ownTags := tagsFor(event, rid, ownState)
			color := viewer.Settings.EventColor(ownTags)
			result = append(result, Occurrence{
				ID: event.ID + "/" + rid, EventID: event.ID, RID: rid, Version: event.Version,
				Title: instance.Title, Start: instance.Start, End: instance.End,
				AllDay: instance.AllDay, Timezone: instance.Timezone,
				Description: instance.Description, Location: instance.Location, URL: instance.URL,
				Members: event.Members, Tags: ownTags, Color: color.Color, Stripe: color.Stripe,
				Editable: event.CanEdit(viewer.ID), Busy: busy, Cancelled: instance.Cancelled,
			})
		}
	}
	slices.SortFunc(result, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
	return result, nil
}

func (a *App) putEvent(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	user := currentUser(c)
	var event Event
	if err := c.Bind(&event); err != nil {
		return err
	}
	id, rid := c.Param("id"), c.QueryParam("rid")
	old := Event{}
	if id != "" {
		var err error
		old, err = a.event(id)
		if err != nil || !old.IsMember(user.ID) {
			return echo.ErrNotFound
		}
		if !old.CanEdit(user.ID) {
			return echo.ErrForbidden
		}
		if event.Version != old.Version {
			return echo.NewHTTPError(409, "event changed; reload before saving")
		}
	} else {
		if rid != "" {
			return echo.NewHTTPError(400, "occurrence needs a series")
		}
		id = newID()
		others := slices.DeleteFunc(event.Members, func(member Member) bool { return member.User == user.ID })
		event.Members = append([]Member{{User: user.ID, Editor: true}}, others...)
	}
	event.Creator = old.Creator
	if old.ID == "" {
		event.Creator = user.ID
	}
	if event.EditPolicy == "" {
		event.EditPolicy = old.EditPolicy
	}
	if event.EditPolicy != "" {
		if event.EditPolicy != "all" && event.EditPolicy != "author" {
			return echo.NewHTTPError(400, "unknown edit policy")
		}
		if !event.IsMember(event.Creator) {
			event.Members = append(event.Members, Member{User: event.Creator})
		}
		for i := range event.Members {
			event.Members[i].Editor = event.EditPolicy == "all" || event.Members[i].User == event.Creator
		}
	}
	event.ID, event.Source, event.UID = id, "", old.UID
	if event.UID == "" {
		event.UID = newID()
	}
	event.Version = old.Version + 1
	event.Categories = nil
	if len(event.Overrides) != 0 {
		return echo.NewHTTPError(400, "edit occurrences separately")
	}
	event.Overrides = old.Overrides
	if err := event.Validate(); err != nil {
		return badRequest(err)
	}
	for _, member := range event.Members {
		if _, err := a.user(member.User); err != nil {
			return echo.NewHTTPError(400, "unknown participant")
		}
	}
	previousMembers := slices.Clone(old.Members)
	if rid != "" {
		date, err := time.Parse(time.RFC3339, rid)
		if err != nil {
			return badRequest(err)
		}
		instances, err := expand(old, date.Add(-time.Second), date.Add(time.Second))
		if err != nil {
			return badRequest(err)
		}
		_, exists := instances[rid]
		if !exists && old.Overrides[rid] == nil {
			return echo.NewHTTPError(400, "unknown occurrence")
		}
		if event.RRule != old.RRule {
			return echo.NewHTTPError(400, "edit recurrence on the series")
		}
		if old.Overrides == nil {
			old.Overrides = map[string]*Event{}
		}
		event.Overrides = nil
		override := event
		old.Overrides[rid] = &override
		old.Members, old.Version, old.EditPolicy = event.Members, event.Version, event.EditPolicy
		event = old
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&event).Error; err != nil {
			return err
		}
		for _, member := range event.Members {
			if slices.ContainsFunc(previousMembers, func(previous Member) bool { return previous.User == member.User }) {
				continue
			}
			var participant User
			if err := tx.First(&participant, "id = ?", member.User).Error; err != nil {
				return err
			}
			tags := participant.Settings.IncomingTags
			if old.ID == "" && member.User == user.ID {
				tags = participant.Settings.OwnTags
			}
			personal := Personal{EventID: id, UserID: member.User, RID: ""}
			if err := tx.Where("event_id = ? AND user_id = ? AND rid = ?", id, member.User, "").Attrs(Personal{Tags: Tags{Add: tags}}).FirstOrCreate(&personal).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.JSON(200, event)
}

func (a *App) deleteEvent(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	user := currentUser(c)
	event, err := a.event(c.Param("id"))
	if err != nil || !event.IsMember(user.ID) {
		return echo.ErrNotFound
	}
	if !event.CanEdit(user.ID) {
		return echo.ErrForbidden
	}
	if c.QueryParam("version") != fmt.Sprint(event.Version) {
		return echo.NewHTTPError(409, "event changed; reload before deleting")
	}
	if rid := c.QueryParam("rid"); rid != "" {
		date, err := time.Parse(time.RFC3339, rid)
		if err != nil {
			return badRequest(err)
		}
		event.ExDates = append(event.ExDates, date)
		delete(event.Overrides, rid)
	} else {
		event.Removed = true
	}
	event.Version++
	if err := a.db.Save(&event).Error; err != nil {
		return err
	}
	return c.JSON(200, echo.Map{"ok": true})
}

func (a *App) putTags(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	user := currentUser(c)
	event, err := a.event(c.Param("id"))
	if err != nil || !event.IsMember(user.ID) {
		return echo.ErrNotFound
	}
	var tags Tags
	if err := c.Bind(&tags); err != nil {
		return err
	}
	if err := validateTags(append(slices.Clone(tags.Add), tags.Remove...), false); err != nil {
		return badRequest(err)
	}
	rid := c.QueryParam("rid")
	if rid != "" {
		date, err := time.Parse(time.RFC3339, rid)
		if err != nil {
			return badRequest(err)
		}
		instances, err := expand(event, date.Add(-time.Second), date.Add(time.Second))
		if err != nil {
			return badRequest(err)
		}
		if _, exists := instances[rid]; !exists && event.Overrides[rid] == nil {
			return echo.NewHTTPError(400, "unknown occurrence")
		}
	}
	if err := a.saveTags(event.ID, user.ID, rid, tags); err != nil {
		return err
	}
	return c.JSON(200, tags)
}
