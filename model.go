package main

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teambition/rrule-go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type Rule struct {
	Op       string `json:"op"`
	Tag      string `json:"tag,omitempty"`
	Children []Rule `json:"children,omitempty"`
}

func (r Rule) Match(tags []string) bool {
	switch r.Op {
	case "true":
		return true
	case "false":
		return false
	case "tag":
		return slices.Contains(tags, r.Tag)
	case "not":
		return len(r.Children) == 1 && !r.Children[0].Match(tags)
	case "and":
		for _, c := range r.Children {
			if !c.Match(tags) {
				return false
			}
		}
		return true
	case "or":
		for _, c := range r.Children {
			if c.Match(tags) {
				return true
			}
		}
	}
	return false
}

func (r Rule) Validate(depth int) error {
	if depth > 20 || len(r.Children) > 100 {
		return problem("rule_too_large")
	}
	switch r.Op {
	case "true", "false":
		if len(r.Children) > 0 || r.Tag != "" {
			return problem("invalid_constant_rule")
		}
	case "tag":
		if r.Tag == "" || len(r.Children) > 0 {
			return problem("tag_required")
		}
	case "not":
		if len(r.Children) != 1 {
			return problem("invalid_not_rule")
		}
	case "and", "or":
	default:
		return problem("unknown_operator")
	}
	for _, c := range r.Children {
		if err := c.Validate(depth + 1); err != nil {
			return err
		}
	}
	return nil
}

type ColorRule struct {
	Rule   Rule   `json:"rule"`
	Color  string `json:"color"`
	Stripe string `json:"stripe,omitempty"`
}

type Settings struct {
	Config       string      `json:"config,omitempty"`
	Tags         []string    `json:"tags"`
	OwnTags      []string    `json:"ownTags"`
	IncomingTags []string    `json:"incomingTags"`
	Busy         Rule        `json:"busy"`
	Colors       []ColorRule `json:"colors"`
	Poll         int         `json:"poll"`
	Timezone     string      `json:"timezone"`
}

func defaults() Settings {
	return Settings{
		Tags: []string{"Important"}, OwnTags: []string{}, IncomingTags: []string{},
		Busy: Rule{Op: "true"}, Colors: []ColorRule{{Rule: Rule{Op: "true"}, Color: "teal"}},
		Poll: 15, Timezone: "",
	}
}

func (s Settings) Validate() error {
	if s.Poll < 5 || s.Poll > 3600 {
		return problem("invalid_poll")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return err
	}
	if err := s.Busy.Validate(0); err != nil {
		return err
	}
	for _, c := range s.Colors {
		if err := c.Rule.Validate(0); err != nil {
			return err
		}
		if !validColor(c.Color) || (c.Stripe != "" && !validColor(c.Stripe)) {
			return problem("invalid_color")
		}
	}
	if len(s.Colors) == 0 || s.Colors[len(s.Colors)-1].Rule.Op != "true" {
		return problem("fallback_color_required")
	}
	return validateTags(append(append(slices.Clone(s.Tags), s.OwnTags...), s.IncomingTags...), false)
}

func validColor(s string) bool {
	if slices.Contains([]string{"dark", "gray", "grey", "red", "pink", "grape", "violet", "indigo", "blue", "cyan", "teal", "green", "lime", "yellow", "orange"}, s) {
		return true
	}
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func (s Settings) EventColor(tags []string) ColorRule {
	for _, c := range s.Colors {
		if c.Rule.Match(tags) {
			return c
		}
	}
	return ColorRule{} // Valid settings always end with an unconditional rule.
}

type User struct {
	Subject  string   `json:"-" gorm:"uniqueIndex"`
	ID       string   `json:"id"`
	Login    string   `json:"login"`
	Name     string   `json:"name"`
	Settings Settings `json:"settings" gorm:"serializer:json"`
}

type Member struct {
	User string `json:"user"`
}

type Event struct {
	TaskID      string            `json:"taskId,omitempty"`
	EditPolicy  string            `json:"editPolicy,omitempty"`
	Creator     string            `json:"creator"`
	Removed     bool              `json:"-"`
	ID          string            `json:"id"`
	UID         string            `json:"uid,omitempty"`
	Source      string            `json:"source,omitempty"`
	Version     int               `json:"version"`
	Title       string            `json:"title"`
	Start       time.Time         `json:"start"`
	End         time.Time         `json:"end"`
	Timezone    string            `json:"timezone"`
	AllDay      bool              `json:"allDay"`
	Description string            `json:"description"`
	Location    string            `json:"location"`
	URL         string            `json:"url"`
	Cancelled   bool              `json:"cancelled"`
	RRule       string            `json:"rrule"`
	RDates      []time.Time       `json:"rdates,omitempty" gorm:"serializer:json"`
	ExDates     []time.Time       `json:"exdates,omitempty" gorm:"serializer:json"`
	Categories  []string          `json:"categories,omitempty" gorm:"serializer:json"`
	Members     []Member          `json:"members" gorm:"serializer:json"`
	Overrides   map[string]*Event `json:"overrides,omitempty" gorm:"serializer:json"`
}

type Tags struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type Participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Occurrence struct {
	TaskID       string        `json:"taskId,omitempty"`
	Participants []Participant `json:"participants,omitempty"`
	ID           string        `json:"id"`
	EventID      string        `json:"eventId,omitempty"`
	RID          string        `json:"rid,omitempty"`
	Version      int           `json:"version,omitempty"`
	Title        string        `json:"title,omitempty"`
	Start        time.Time     `json:"start"`
	End          time.Time     `json:"end"`
	AllDay       bool          `json:"allDay"`
	Timezone     string        `json:"timezone,omitempty"`
	Description  string        `json:"description,omitempty"`
	Location     string        `json:"location,omitempty"`
	URL          string        `json:"url,omitempty"`
	Members      []Member      `json:"members,omitempty" gorm:"serializer:json"`
	Tags         []string      `json:"tags,omitempty"`
	Color        string        `json:"color"`
	Stripe       string        `json:"stripe,omitempty"`
	Editable     bool          `json:"editable,omitempty"`
	Busy         bool          `json:"busy,omitempty"`
	Cancelled    bool          `json:"cancelled,omitempty"`
}

func (e Event) IsMember(user string) bool {
	return slices.ContainsFunc(e.Members, func(m Member) bool { return m.User == user })
}

func (e Event) CanEdit(user string) bool {
	return e.Source == "" && e.IsMember(user) && (e.EditPolicy == "all" || e.Creator == user)
}

func (e Event) Validate() error {
	if e.Start.IsZero() || !e.End.After(e.Start) || e.End.Sub(e.Start) > 366*24*time.Hour {
		return problem("invalid_event_range")
	}
	loc, err := time.LoadLocation(e.Timezone)
	if err != nil {
		return err
	}
	if e.AllDay {
		for _, t := range []time.Time{e.Start, e.End} {
			v := t.In(loc)
			if v.Hour() != 0 || v.Minute() != 0 || v.Second() != 0 {
				return problem("invalid_all_day_range")
			}
		}
	}
	if e.RRule != "" {
		o, err := rrule.StrToROptionInLocation(e.RRule, loc)
		if err != nil {
			return err
		}
		if o.Freq > rrule.DAILY {
			return problem("invalid_frequency")
		}
		o.Dtstart = e.Start.In(loc)
		if _, err = rrule.NewRRule(*o); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, m := range e.Members {
		if seen[m.User] || m.User == "" {
			return problem("invalid_participant")
		}
		seen[m.User] = true
	}
	if e.EditPolicy != "all" && e.EditPolicy != "author" {
		return problem("invalid_edit_policy")
	}
	if !e.IsMember(e.Creator) {
		return problem("creator_required")
	}
	return nil
}

func newID() string { return uuid.NewString() }

func validateTags(tags []string, imported bool) error {
	if len(tags) > 200 {
		return problem("too_many_tags")
	}
	for _, t := range tags {
		if strings.TrimSpace(t) == "" || len(t) > 200 || (!imported && strings.HasPrefix(t, "ics:")) {
			return problem("invalid_tag")
		}
	}
	return nil
}

func unique(tags []string) []string {
	out := []string{}
	for _, t := range tags {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

type Personal struct {
	EventID string `gorm:"primaryKey"`
	UserID  string `gorm:"primaryKey"`
	RID     string `gorm:"primaryKey;column:rid"`
	Tags    Tags   `gorm:"serializer:json"`
}

func openDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	conn, err := db.DB()
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&User{}, &Event{}, &Personal{}, &Source{}, &Export{}, &Task{}); err != nil {
		conn.Close()
		return nil, err
	}
	if err = migrateLegacy(db); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (a *App) user(id string) (User, error) {
	var user User
	err := a.db.First(&user, "id = ?", id).Error
	return user, err
}

func (a *App) event(id string) (Event, error) {
	var event Event
	err := a.db.First(&event, "id = ? AND removed = ?", id, false).Error
	return event, err
}

func (a *App) events(user string) ([]Event, error) {
	events := []Event{}
	err := a.db.Where("removed = ? AND EXISTS (SELECT 1 FROM json_each(events.members) WHERE json_extract(value, '$.user') = ?)", false, user).Find(&events).Error
	return events, err
}

func (a *App) tagState(event, user string) (map[string]Tags, error) {
	var rows []Personal
	err := a.db.Where("event_id = ? AND user_id = ?", event, user).Find(&rows).Error
	result := map[string]Tags{}
	for _, row := range rows {
		result[row.RID] = row.Tags
	}
	return result, err
}

// Fetch personal tags once per user, rather than once per event.
func (a *App) tagStates(user string) (map[string]map[string]Tags, error) {
	var rows []Personal
	err := a.db.Where("user_id = ?", user).Find(&rows).Error
	result := map[string]map[string]Tags{}
	for _, row := range rows {
		if result[row.EventID] == nil {
			result[row.EventID] = map[string]Tags{}
		}
		result[row.EventID][row.RID] = row.Tags
	}
	return result, err
}

func tagsFor(e Event, rid string, state map[string]Tags) []string {
	cat := e.Categories
	if o := e.Overrides[rid]; o != nil {
		cat = o.Categories
	}
	out := append(slices.Clone(cat), state[""].Add...)
	for _, t := range state[""].Remove {
		out = slices.DeleteFunc(out, func(s string) bool { return t == s })
	}
	if rid != "" {
		out = append(out, state[rid].Add...)
		for _, t := range state[rid].Remove {
			out = slices.DeleteFunc(out, func(s string) bool { return s == t })
		}
	}
	return unique(out)
}

func (a *App) saveTags(event, user, rid string, tags Tags) error {
	return a.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&Personal{EventID: event, UserID: user, RID: rid, Tags: tags}).Error
}

func recurrenceKey(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func instance(e Event, t time.Time) Event {
	rid := recurrenceKey(t)
	if o := e.Overrides[rid]; o != nil {
		return *o
	}
	result := e
	result.Overrides = nil
	result.Start = t
	result.End = t.Add(e.End.Sub(e.Start))
	if e.AllDay {
		loc, _ := time.LoadLocation(e.Timezone)
		start := e.Start.In(loc)
		end := e.End.In(loc)
		days := int(time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).Sub(time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24)
		result.End = t.In(loc).AddDate(0, 0, days)
	}
	return result
}

func expand(e Event, from, to time.Time) (map[string]Event, error) {
	out := map[string]Event{}
	add := func(t time.Time) {
		rid := ""
		if e.RRule != "" || len(e.RDates) > 0 || len(e.Overrides) > 0 {
			rid = recurrenceKey(t)
		}
		x := instance(e, t)
		if x.Start.Before(to) && x.End.After(from) {
			out[rid] = x
		}
	}
	excluded := func(t time.Time) bool {
		return slices.ContainsFunc(e.ExDates, func(x time.Time) bool { return x.Equal(t) })
	}
	if e.RRule == "" {
		if !excluded(e.Start) {
			add(e.Start)
		}
	} else {
		loc, _ := time.LoadLocation(e.Timezone)
		opt, err := rrule.StrToROptionInLocation(e.RRule, loc)
		if err != nil {
			return nil, err
		}
		opt.Dtstart = e.Start.In(loc)
		r, err := rrule.NewRRule(*opt)
		if err != nil {
			return nil, err
		}
		// Limit input frequency and iteration to bound work on hostile ICS files.
		next := r.Iterator()
		for n := 0; ; n++ {
			if n >= 200000 {
				return nil, problem("recurrence_limit")
			}
			t, ok := next()
			if !ok || !t.Before(to) {
				break
			}
			if t.Add(e.End.Sub(e.Start) + 24*time.Hour).Before(from) {
				continue
			}
			if !excluded(t) {
				add(t)
			}
		}
	}
	for _, t := range e.RDates {
		if !excluded(t) {
			add(t)
		}
	}
	for rid, o := range e.Overrides {
		if o.Start.Before(to) && o.End.After(from) {
			out[rid] = *o
		}
	}
	return out, nil
}

func publicID(export, event, rid string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(export+"/"+event+"/"+rid)).String()
}
