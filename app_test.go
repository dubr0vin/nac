package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ical "github.com/emersion/go-ical"
)

func testApp(t *testing.T) *App {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "nac.db"))
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	t.Cleanup(func() { connection.Close() })
	return &App{db: db, client: &http.Client{Timeout: time.Second}, trusted: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
}

func request(t *testing.T, app *App, user, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-NAC", "1")
	if user != "" {
		r.Header.Set("Remote-Sub", user)
		r.Header.Set("Remote-User", user)
		r.Header.Set("Remote-Name", user)
	}
	w := httptest.NewRecorder()
	handler := app.handler()
	handler.Debug = true
	handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func login(t *testing.T, app *App, subject string) User {
	state := decode[struct{ Me User }](t, request(t, app, subject, "GET", "/api/state", nil, 200))
	return state.Me
}

func sampleEvent() Event {
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	return Event{Title: "Private meeting", Start: start, End: start.Add(time.Hour), Timezone: "UTC", Description: "Secret details"}
}

const rangeQuery = "?from=2026-10-01T00:00:00Z&to=2026-11-01T00:00:00Z"

func TestIdentityPermissionsAndPersonalTags(t *testing.T) {
	app := testApp(t)
	alice, bob, eve := login(t, app, "alice"), login(t, app, "bob"), login(t, app, "eve")
	if login(t, app, "alice").ID != alice.ID {
		t.Fatal("repeat login created another user")
	}
	request(t, app, "", "GET", "/api/state", nil, 401)
	spoof := httptest.NewRequest("GET", "/api/state", nil)
	spoof.RemoteAddr = "192.0.2.1:1234"
	spoof.Header.Set("Remote-Sub", "alice")
	w := httptest.NewRecorder()
	app.handler().ServeHTTP(w, spoof)
	if w.Code != 401 {
		t.Fatal("untrusted peer supplied identity")
	}

	input := sampleEvent()
	input.Members = []Member{{User: bob.ID, Editor: true}}
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", input, 200))
	request(t, app, "alice", "PUT", "/api/events/"+event.ID+"/tags", Tags{Add: []string{"work"}}, 200)
	request(t, app, "bob", "PUT", "/api/events/"+event.ID+"/tags", Tags{Add: []string{"friends"}}, 200)
	request(t, app, "eve", "GET", "/api/events/"+event.ID, nil, 404)
	request(t, app, "eve", "PUT", "/api/events/"+event.ID, event, 404)
	busy := request(t, app, "eve", "GET", "/api/events"+rangeQuery+"&user="+alice.ID, nil, 200)
	for _, secret := range []string{input.Title, input.Description, "work", event.ID, bob.ID} {
		if strings.Contains(busy.Body.String(), secret) {
			t.Fatalf("availability leaked %q", secret)
		}
	}
	own := decode[[]Occurrence](t, request(t, app, "bob", "GET", "/api/events"+rangeQuery+"&user="+alice.ID, nil, 200))
	if len(own) != 1 || !slices.Contains(own[0].Tags, "friends") || slices.Contains(own[0].Tags, "work") {
		t.Fatal("personal tags mixed between users", own)
	}

	// An editor can change membership and revoke the original creator's rights.
	event.Members = []Member{{User: bob.ID, Editor: true}, {User: eve.ID, Editor: false}}
	updated := decode[Event](t, request(t, app, "bob", "PUT", "/api/events/"+event.ID, event, 200))
	request(t, app, "alice", "GET", "/api/events/"+event.ID, nil, 404)
	request(t, app, "eve", "PUT", "/api/events/"+event.ID, updated, 403)
	request(t, app, "bob", "PUT", "/api/events/"+event.ID, event, 409)
	updated.Members[0].Editor = false
	request(t, app, "bob", "PUT", "/api/events/"+event.ID, updated, 400)
}

func TestRulesAndSettings(t *testing.T) {
	for _, test := range []struct {
		rule Rule
		want bool
	}{
		{Rule{Op: "true"}, true},
		{Rule{Op: "false"}, false},
		{Rule{Op: "not", Children: []Rule{{Op: "false"}}}, true},
		{Rule{Op: "and", Children: []Rule{{Op: "true"}, {Op: "false"}}}, false},
		{Rule{Op: "or", Children: []Rule{{Op: "false"}, {Op: "true"}}}, true},
	} {
		if err := test.rule.Validate(0); err != nil || test.rule.Match([]string{"work"}) != test.want {
			t.Fatalf("constant rule %+v: match or validation failed: %v", test.rule, err)
		}
	}
	if (Rule{Op: "true", Children: []Rule{{Op: "false"}}}).Validate(0) == nil {
		t.Fatal("constant rule accepted hidden children")
	}
	if err := defaults().Validate(); err != nil {
		t.Fatalf("device timezone default is invalid: %v", err)
	}
	rule := Rule{Op: "and", Children: []Rule{
		{Op: "tag", Tag: "call"}, {Op: "not", Children: []Rule{{Op: "tag", Tag: "work"}}},
	}}
	if !rule.Match([]string{"call"}) || rule.Match([]string{"call", "work"}) || rule.Match(nil) {
		t.Fatal("nested conditions are incorrect")
	}
	if !(Rule{Op: "and"}).Match(nil) || (Rule{Op: "or"}).Match(nil) {
		t.Fatal("empty expression semantics")
	}
	settings := defaults()
	settings.Colors = []ColorRule{{Rule: rule, Color: "#ff0000"}, {Rule: Rule{Op: "and"}, Color: "#0000ff"}}
	if settings.EventColor([]string{"call"}).Color != "#ff0000" {
		t.Fatal("first matching color did not win")
	}
	app := testApp(t)
	user := login(t, app, "alice")
	settings.Poll = 37
	settings.Busy = Rule{Op: "false"}
	request(t, app, "alice", "PUT", "/api/settings", settings, 200)
	loaded, err := app.user(user.ID)
	if err != nil || loaded.Settings.Poll != 37 {
		t.Fatal("settings did not persist", err)
	}
	request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200)
	login(t, app, "bob")
	busy := decode[[]Occurrence](t, request(t, app, "bob", "GET", "/api/events"+rangeQuery+"&user="+user.ID, nil, 200))
	if len(busy) != 0 {
		t.Fatal("availability ignored owner's rule")
	}
}

func snapshot(title string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nBEGIN:VEVENT\r\nUID:same-uid\r\nDTSTAMP:20261001T000000Z\r\nDTSTART:20261002T090000Z\r\nDTEND:20261002T100000Z\r\nSUMMARY:" + title + "\r\nCATEGORIES:Work,Meeting\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func TestSnapshotImportAndTokens(t *testing.T) {
	app := testApp(t)
	user := login(t, app, "alice")
	source := decode[Source](t, request(t, app, "alice", "POST", "/api/sources", Source{
		Name: "Work", Timezone: "UTC", Tags: []string{"source"}, Interval: 900,
	}, 200))
	source.User = user.ID
	if err := app.importSnapshot(source, strings.NewReader(snapshot("First"))); err != nil {
		t.Fatal(err)
	}
	events, _ := app.events(user.ID)
	if len(events) != 1 || !slices.Contains(events[0].Categories, "ics:Work") {
		t.Fatal("categories not mapped", events)
	}
	id := events[0].ID
	request(t, app, "alice", "PUT", "/api/events/"+id+"/tags", Tags{Add: []string{"personal"}}, 200)
	for _, body := range []string{snapshot("Updated"), snapshot("Updated")} {
		if err := app.importSnapshot(source, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	events, _ = app.events(user.ID)
	if len(events) != 1 || events[0].ID != id || events[0].Title != "Updated" {
		t.Fatal("snapshot identity changed")
	}
	if err := app.importSnapshot(source, strings.NewReader("not an ICS file")); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	events, _ = app.events(user.ID)
	if len(events) != 1 {
		t.Fatal("invalid snapshot deleted data")
	}
	empty := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nEND:VCALENDAR\r\n"
	if err := app.importSnapshot(source, strings.NewReader(empty)); err != nil {
		t.Fatal(err)
	}
	events, _ = app.events(user.ID)
	if len(events) != 0 {
		t.Fatal("empty snapshot did not clear source")
	}
	if err := app.importSnapshot(source, strings.NewReader(snapshot("Returned"))); err != nil {
		t.Fatal(err)
	}
	events, _ = app.events(user.ID)
	tags, _ := app.tagState(events[0].ID, user.ID)
	if events[0].ID != id || !slices.Contains(tagsFor(events[0], "", tags), "personal") {
		t.Fatal("reappearing UID lost personal tags")
	}

	token := decode[map[string]string](t, request(t, app, "alice", "POST", "/api/sources/"+source.ID+"/token", nil, 200))["token"]
	push := func(token string, want int) {
		r := httptest.NewRequest("POST", "/api/sources/"+source.ID+"/import", strings.NewReader(snapshot("Pushed")))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler := app.handler()
		handler.Debug = true
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("push: %d %s", w.Code, w.Body.String())
		}
	}
	push(token, 200)
	request(t, app, "alice", "DELETE", "/api/sources/"+source.ID+"/token", nil, 200)
	push(token, 401)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(snapshot("Pulled")))
	}))
	defer server.Close()
	source.URL = server.URL
	request(t, app, "alice", "PUT", "/api/sources/"+source.ID, source, 200)
	if err := app.pull(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	events, _ = app.events(user.ID)
	if len(events) != 1 || events[0].ID != id || events[0].Title != "Pulled" {
		t.Fatal("pull did not use same identity/import semantics")
	}
}

func TestRecurrenceDSTOverridesAndExport(t *testing.T) {
	app := testApp(t)
	user := login(t, app, "alice")
	zone, _ := time.LoadLocation("Europe/Berlin")
	input := sampleEvent()
	input.Start = time.Date(2026, 10, 24, 9, 0, 0, 0, zone)
	input.End = input.Start.Add(time.Hour)
	input.Timezone = zone.String()
	input.RRule = "FREQ=DAILY;COUNT=3"
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", input, 200))
	instances, err := expand(event, input.Start.Add(-time.Hour), input.Start.AddDate(0, 0, 4))
	if err != nil || len(instances) != 3 {
		t.Fatal("recurrence expansion", instances, err)
	}
	for _, occurrence := range instances {
		if occurrence.Start.In(zone).Hour() != 9 {
			t.Fatal("DST shifted recurring wall time")
		}
	}
	rid := recurrenceKey(input.Start.AddDate(0, 0, 1))
	request(t, app, "alice", "PUT", "/api/events/"+event.ID+"/tags?rid="+rid, Tags{Add: []string{"selected"}}, 200)
	feed := decode[Export](t, request(t, app, "alice", "POST", "/api/exports", Export{
		Name: "Availability", Rule: Rule{Op: "tag", Tag: "selected"}, Fields: []string{},
		View: "week", Theme: "auto", Timezone: "Europe/Berlin", Poll: 12,
	}, 200))
	ics := request(t, app, "", "GET", "/export.ics?id="+feed.ID, nil, 200)
	for _, hidden := range []string{"SUMMARY", "DESCRIPTION", event.ID, "selected", "CATEGORIES"} {
		if strings.Contains(ics.Body.String(), hidden) {
			t.Fatalf("export leaked %s", hidden)
		}
	}
	calendar, err := ical.NewDecoder(strings.NewReader(ics.Body.String())).Decode()
	if err != nil || len(calendar.Events()) != 1 {
		t.Fatal("filtered occurrence export", err)
	}
	view := request(t, app, "", "GET", "/view/data"+rangeQuery+"&id="+feed.ID, nil, 200)
	if strings.Contains(view.Body.String(), "selected") || strings.Contains(view.Body.String(), input.Title) {
		t.Fatal("view leaked rule or title")
	}
	public := decode[struct{ Events []Occurrence }](t, view)
	if len(public.Events) != 1 {
		t.Fatal("view/ICS filters disagree")
	}
	rotated := decode[Export](t, request(t, app, "alice", "POST", "/api/exports/"+feed.ID+"/rotate", nil, 200))
	request(t, app, "", "GET", "/export.ics?id="+feed.ID, nil, 404)
	request(t, app, "", "GET", "/view/data"+rangeQuery+"&id="+feed.ID, nil, 404)
	request(t, app, "", "GET", "/export.ics?id="+rotated.ID, nil, 200)

	allDay := sampleEvent()
	allDay.Timezone, allDay.AllDay = zone.String(), true
	allDay.Start = time.Date(2026, 10, 24, 0, 0, 0, 0, zone)
	allDay.End = allDay.Start.AddDate(0, 0, 1)
	allDay.RRule = "FREQ=DAILY;COUNT=3"
	allDay.Members = []Member{{User: user.ID, Editor: true}}
	instances, err = expand(allDay, allDay.Start, allDay.Start.AddDate(0, 0, 4))
	if err != nil {
		t.Fatal(err)
	}
	for _, occurrence := range instances {
		if occurrence.Start.In(zone).Hour() != 0 || occurrence.End.In(zone).Hour() != 0 {
			t.Fatal("all-day event no longer ends at local midnight")
		}
	}
}

func TestOccurrenceEditsAndSourceIsolation(t *testing.T) {
	app := testApp(t)
	alice, bob := login(t, app, "alice"), login(t, app, "bob")
	input := sampleEvent()
	input.RRule = "FREQ=DAILY;COUNT=3"
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", input, 200))
	rid := recurrenceKey(input.Start.AddDate(0, 0, 1))
	override := event
	override.Title = "Moved occurrence"
	override.Start = input.Start.AddDate(0, 0, 1).Add(3 * time.Hour)
	override.End = override.Start.Add(time.Hour)
	override.Members = append(override.Members, Member{User: bob.ID, Editor: true})
	event = decode[Event](t, request(t, app, "alice", "PUT", "/api/events/"+event.ID+"?rid="+rid, override, 200))
	instances, err := expand(event, input.Start.Add(-time.Hour), input.Start.AddDate(0, 0, 4))
	if err != nil || len(instances) != 3 || instances[rid].Title != override.Title || instances[rid].Start.Hour() != 12 {
		t.Fatal("occurrence override did not replace original", instances, err)
	}
	request(t, app, "bob", "GET", "/api/events/"+event.ID, nil, 200)
	request(t, app, "bob", "DELETE", "/api/events/"+event.ID+"?rid="+rid+"&version=2", nil, 200)
	event, _ = app.event(event.ID)
	instances, err = expand(event, input.Start.Add(-time.Hour), input.Start.AddDate(0, 0, 4))
	if err != nil || len(instances) != 2 {
		t.Fatal("occurrence deletion removed wrong events", instances, err)
	}

	for _, user := range []User{alice, bob} {
		source := Source{ID: newID(), User: user.ID, Name: "same UID", Timezone: "UTC"}
		if err := app.db.Create(&source).Error; err != nil {
			t.Fatal(err)
		}
		if err := app.importSnapshot(source, strings.NewReader(snapshot(user.Name))); err != nil {
			t.Fatal(err)
		}
	}
	aliceEvents, _ := app.events(alice.ID)
	bobEvents, _ := app.events(bob.ID)
	var aliceImport, bobImport Event
	for _, event := range aliceEvents {
		if event.Source != "" {
			aliceImport = event
		}
	}
	for _, event := range bobEvents {
		if event.Source != "" {
			bobImport = event
		}
	}
	if aliceImport.ID == bobImport.ID || aliceImport.Title != "alice" || bobImport.Title != "bob" {
		t.Fatal("UID collision between sources")
	}
	request(t, app, "bob", "GET", "/api/events/"+aliceImport.ID, nil, 404)
	request(t, app, "alice", "PUT", "/api/events/"+aliceImport.ID, aliceImport, 403)
}

func TestExportRecurrenceWhitelistAndFramePolicy(t *testing.T) {
	app := testApp(t)
	alice := login(t, app, "alice")
	input := sampleEvent()
	input.RRule = "FREQ=DAILY;COUNT=3"
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", input, 200))
	rid := recurrenceKey(input.Start.AddDate(0, 0, 1))
	request(t, app, "alice", "PUT", "/api/events/"+event.ID+"/tags", Tags{Add: []string{"visible"}}, 200)
	request(t, app, "alice", "PUT", "/api/events/"+event.ID+"/tags?rid="+rid, Tags{Remove: []string{"visible"}}, 200)
	feed := Export{ID: newID(), User: alice.ID, Name: "Test", Rule: Rule{Op: "tag", Tag: "visible"}, Fields: []string{"title"}, View: "week", Theme: "auto", Timezone: "UTC", Poll: 5}
	if err := app.db.Create(&feed).Error; err != nil {
		t.Fatal(err)
	}
	data, err := app.calendar(feed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("RRULE:FREQ=DAILY;COUNT=3")) || !bytes.Contains(data, []byte("EXDATE")) || bytes.Contains(data, []byte(input.Description)) {
		t.Fatal("recurrence or field selection lost", string(data))
	}
	projected, err := parseICS(bytes.NewReader(data), Source{ID: "roundtrip", User: alice.ID, Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range projected {
		instances, err := expand(item, input.Start.Add(-time.Hour), input.Start.AddDate(0, 0, 4))
		if err != nil || len(instances) != 2 {
			t.Fatal("export includes excluded occurrence", err)
		}
	}
	page := request(t, app, "", "GET", "/view?id="+feed.ID, nil, 200)
	if page.Header().Get("Content-Security-Policy") != "" {
		t.Fatal("iframe view blocked")
	}
	page = request(t, app, "", "GET", "/", nil, 200)
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("main UI should not be framed")
	}
}

func TestPersistenceAndInvalidSnapshotAtomicity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{db: db, trusted: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}
	user := login(t, app, "alice")
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200))
	connection, _ := db.DB()
	connection.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	connection, _ = db.DB()
	defer connection.Close()
	app.db = db
	if login(t, app, "alice").ID != user.ID {
		t.Fatal("user identity changed on restart")
	}
	request(t, app, "alice", "GET", "/api/events/"+event.ID, nil, 200)
	source := Source{ID: newID(), User: user.ID, Name: "Atomic", Timezone: "UTC"}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.importSnapshot(source, strings.NewReader(snapshot("Before"))); err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(snapshot("After"), "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:broken\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	if err := app.importSnapshot(source, strings.NewReader(invalid)); err == nil {
		t.Fatal("partially invalid snapshot accepted")
	}
	var imported Event
	if err := db.First(&imported, "source = ?", source.ID).Error; err != nil || imported.Title != "Before" {
		t.Fatal("partially applied invalid snapshot", err)
	}
}

func TestYAMLPreferencesAndStripes(t *testing.T) {
	app := testApp(t)
	alice := login(t, app, "alice")
	text := `# Preserve my comments.
tags: [work, personal]
default_tags:
  created: [work]
  invited: [personal]
busy:
  and:
    - tag: work
    - not: {tag: personal}
colors:
  - when: {tag: work}
    color: ["#4263eb", "#e8590c"]
  - when: true
    color: "#868e96"
`
	body := map[string]any{"yaml": text, "timezone": "Europe/Moscow", "poll": 23}
	settings := decode[Settings](t, request(t, app, "alice", "PUT", "/api/settings/config", body, 200))
	if settings.Config != text || settings.Poll != 23 || settings.Timezone != "Europe/Moscow" || !settings.Busy.Match([]string{"work"}) || settings.Busy.Match([]string{"work", "personal"}) {
		t.Fatal("YAML settings or independent UI fields were not applied")
	}
	paint := settings.EventColor([]string{"work"})
	if paint.Color != "#4263eb" || paint.Stripe != "#e8590c" || settings.EventColor(nil).Stripe != "" {
		t.Fatal("striped color or fallback is incorrect")
	}
	request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200)
	events := decode[[]Occurrence](t, request(t, app, "alice", "GET", "/api/events"+rangeQuery, nil, 200))
	if len(events) != 1 || events[0].Stripe != paint.Stripe || events[0].Color != paint.Color {
		t.Fatal("personal color did not reach event API")
	}
	feed := Export{Name: "Availability", Rule: Rule{Op: "true"}, Fields: []string{}, View: "week", Theme: "auto", Timezone: "UTC", Poll: 15}
	feed = decode[Export](t, request(t, app, "alice", "POST", "/api/exports", feed, 200))
	public := decode[struct{ Events []Occurrence }](t, request(t, app, "", "GET", "/view/data?id="+feed.ID+"&"+strings.TrimPrefix(rangeQuery, "?"), nil, 200))
	if len(public.Events) != 1 || public.Events[0].Stripe != paint.Stripe || public.Events[0].Title != "" || len(public.Events[0].Tags) != 0 {
		t.Fatal("kiosk lost stripes or exposed hidden fields")
	}
	for _, invalid := range []string{
		"", "busy: [", text + "typo: true\n", text + "---\nbusy: false\n",
		strings.Replace(text, "tag: work", "unknown: work", 1),
		strings.Replace(text, "tag: work", "tag: 12", 1),
		strings.Replace(text, "#4263eb", "url(https://example.org)", 1),
		strings.Replace(text, "when: true", "when: false", 1),
		strings.Replace(text, "color: [\"#4263eb\", \"#e8590c\"]", "color: []", 1),
		strings.Replace(text, "created: [work]", "typo: [work]", 1),
		strings.Replace(text, "when: true", "when: true\n    typo: value", 1),
	} {
		body["yaml"] = invalid
		request(t, app, "alice", "PUT", "/api/settings/config", body, 400)
	}
	persisted, err := app.user(alice.ID)
	if err != nil || persisted.Settings.Config != text {
		t.Fatal("invalid YAML changed saved settings", err)
	}
	// Legacy JSON settings render as editable YAML without changing their meaning.
	legacy := defaults()
	legacy.Busy = Rule{Op: "and"}
	yamlText, err := settingsYAML(legacy)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := parseSettingsYAML(yamlText, legacy)
	if err != nil || !roundtrip.Busy.Match(nil) || roundtrip.EventColor(nil).Color != legacy.Color {
		t.Fatal("legacy settings did not round-trip", err, yamlText)
	}
}

func TestMantineColorNames(t *testing.T) {
	for _, color := range []string{"dark", "gray", "grey", "red", "pink", "grape", "violet", "indigo", "blue", "cyan", "teal", "green", "lime", "yellow", "orange", "#12aBcF"} {
		if !validColor(color) {
			t.Fatalf("rejected color %q", color)
		}
	}
	for _, color := range []string{"bleu", "blue; background:red", "url(example.org)", "#12", ""} {
		if validColor(color) {
			t.Fatalf("accepted invalid color %q", color)
		}
	}
	text := `tags: []
default_tags: {created: [], invited: []}
busy: true
colors:
  - when: {tag: work}
    color: [blue, orange]
  - when: {tag: personal}
    color: [grey, "#123456"]
  - when: true
    color: grey
`
	settings, err := parseSettingsYAML(text, defaults())
	if err != nil {
		t.Fatal(err)
	}
	if paint := settings.EventColor([]string{"work"}); paint.Color != "blue" || paint.Stripe != "orange" {
		t.Fatal("named stripe colors were lost", paint)
	}
	if paint := settings.EventColor([]string{"personal"}); paint.Color != "grey" || paint.Stripe != "#123456" {
		t.Fatal("mixed name/HEX colors were lost", paint)
	}
	if settings.EventColor(nil).Color != "grey" {
		t.Fatal("named fallback was lost")
	}
}

func TestExportYAMLAndKioskSettings(t *testing.T) {
	app := testApp(t)
	login(t, app, "alice")
	login(t, app, "bob")
	input := Export{
		Name: "Work", Condition: "and:\n  - tag: work\n  - not: {tag: vacation}\n",
		Fields: []string{"title"}, View: "week", Theme: "auto", Timezone: "UTC", Poll: 15,
	}
	feed := decode[Export](t, request(t, app, "alice", "POST", "/api/exports", input, 200))
	if !feed.Rule.Match([]string{"work"}) || feed.Rule.Match([]string{"work", "vacation"}) {
		t.Fatal("export YAML did not produce the expected filter")
	}
	for _, invalid := range []string{"and: [", "true\n---\nfalse", "unknown: true", ""} {
		input.Condition = invalid
		request(t, app, "alice", "PUT", "/api/exports/"+feed.ID, input, 400)
	}
	unchanged, err := app.export(feed.ID)
	if err != nil || unchanged.Condition != feed.Condition {
		t.Fatal("invalid YAML replaced saved filter", err)
	}
	kiosk := Export{
		View: "month", Theme: "dark", Timezone: "Europe/Moscow", Poll: 30,
		Name: "Must not replace name", Condition: "true", Fields: []string{"description"},
	}
	request(t, app, "bob", "PUT", "/api/exports/"+feed.ID+"/kiosk", kiosk, 404)
	request(t, app, "alice", "PUT", "/api/exports/"+feed.ID+"/kiosk", kiosk, 200)
	updated, err := app.export(feed.ID)
	if err != nil || updated.Condition != feed.Condition || updated.Name != feed.Name || !slices.Equal(updated.Fields, feed.Fields) {
		t.Fatal("kiosk settings changed export content", err)
	}
	if updated.View != "month" || updated.Theme != "dark" || updated.Poll != 30 || updated.Timezone != "Europe/Moscow" {
		t.Fatal("kiosk settings were not saved")
	}
	kiosk.Poll = 0
	request(t, app, "alice", "PUT", "/api/exports/"+feed.ID+"/kiosk", kiosk, 400)
	// Existing exports with JSON rule trees are still editable in YAML.
	input.Condition, input.Rule = "", Rule{Op: "tag", Tag: "legacy"}
	legacy := decode[Export](t, request(t, app, "alice", "POST", "/api/exports", input, 200))
	items, err := app.exports(unchanged.User)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == legacy.ID && !strings.Contains(item.Condition, "tag: legacy") {
			t.Fatal("missing legacy YAML condition")
		}
	}
}

func TestEventAuthorPolicyAndBatchAvailability(t *testing.T) {
	app := testApp(t)
	alice, bob, eve := login(t, app, "alice"), login(t, app, "bob"), login(t, app, "eve")
	input := sampleEvent()
	input.Creator = eve.ID
	input.EditPolicy = "author"
	input.Members = []Member{{User: bob.ID, Editor: true}}
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", input, 200))
	if event.Creator != alice.ID || !event.CanEdit(alice.ID) || event.CanEdit(bob.ID) {
		t.Fatal("creator or author-only permissions are incorrect")
	}
	request(t, app, "bob", "PUT", "/api/events/"+event.ID, event, 403)
	event.EditPolicy = "all"
	event = decode[Event](t, request(t, app, "alice", "PUT", "/api/events/"+event.ID, event, 200))
	event.Creator, event.EditPolicy = bob.ID, "author"
	event = decode[Event](t, request(t, app, "bob", "PUT", "/api/events/"+event.ID, event, 200))
	if event.Creator != alice.ID || event.CanEdit(bob.ID) || !event.CanEdit(alice.ID) {
		t.Fatal("editor changed creator identity or retained author-only access")
	}
	for _, viewer := range []string{"bob", "eve"} {
		batch := request(t, app, viewer, "POST", "/api/availability"+rangeQuery, []string{alice.ID, bob.ID}, 200)
		result := decode[map[string][]Occurrence](t, batch)
		if len(result[alice.ID]) != 1 || len(result[bob.ID]) != 1 {
			t.Fatal("missing participant schedules")
		}
		if viewer == "eve" {
			for _, secret := range []string{event.Title, event.Description, event.ID} {
				if strings.Contains(batch.Body.String(), secret) {
					t.Fatalf("availability leaked %q", secret)
				}
			}
		} else if result[alice.ID][0].Title != event.Title {
			t.Fatal("participant lost visible details")
		}
	}
	request(t, app, "", "POST", "/api/availability"+rangeQuery, []string{alice.ID}, 401)
	request(t, app, "alice", "GET", "/event?id="+event.ID, nil, 200)
}
