package main

import (
	"encoding/json"

	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestLegacyMigrationPreservesSettingsAndPermissions(t *testing.T) {
	app := testApp(t)
	alice := login(t, app, "alice")
	bob := login(t, app, "bob")
	event := decode[Event](t, request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200))
	members, _ := json.Marshal([]map[string]any{
		{"user": alice.ID, "editor": true}, {"user": bob.ID, "editor": false},
	})
	app.db.Model(&event).Updates(map[string]any{"edit_policy": "", "creator": "", "members": string(members)})
	app.db.Model(&alice).Update("settings", `{"tags":[],"ownTags":[],"incomingTags":[],"busy":{"op":"true"},"colors":[],"color":"orange","poll":15,"timezone":""}`)
	source := Source{ID: newID(), User: alice.ID, Name: "Legacy", Timezone: "UTC"}
	app.db.Create(&source)
	app.db.Model(&source).Update("error", "source unavailable")
	for i := 0; i < 2; i++ {
		if err := migrateLegacy(app.db); err != nil {
			t.Fatal(err)
		}
		migrated, _ := app.event(event.ID)
		user, _ := app.user(alice.ID)
		loaded, _ := app.source(source.ID)
		if migrated.EditPolicy != "author" || migrated.Creator != alice.ID || !migrated.CanEdit(alice.ID) || migrated.CanEdit(bob.ID) {
			t.Fatal("migration changed access", migrated)
		}
		if user.Settings.EventColor(nil).Color != "orange" || user.Settings.Validate() != nil {
			t.Fatal("migration lost fallback color", user.Settings)
		}
		if loaded.Error == nil || loaded.Error.Params["detail"] != "source unavailable" {
			t.Fatal("migration lost source error", loaded.Error)
		}
	}
	eve := login(t, app, "eve")
	members, _ = json.Marshal([]map[string]any{
		{"user": alice.ID, "editor": true}, {"user": bob.ID, "editor": true}, {"user": eve.ID, "editor": false},
	})
	app.db.Model(&event).Updates(map[string]any{"edit_policy": "", "members": string(members)})
	if migrateLegacy(app.db) == nil {
		t.Fatal("ambiguous legacy access was silently changed")
	}
}

func TestPollingReadsDoNotWriteOrQueryTagsPerEvent(t *testing.T) {
	app := testApp(t)
	login(t, app, "alice")
	writes, reads := 0, 0
	app.db.Callback().Update().After("gorm:update").Register("test_writes", func(*gorm.DB) { writes++ })
	app.db.Callback().Query().After("gorm:query").Register("test_reads", func(*gorm.DB) { reads++ })
	request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200)
	reads, writes = 0, 0
	request(t, app, "alice", "GET", "/api/state", nil, 200)
	request(t, app, "alice", "GET", "/api/events"+rangeQuery, nil, 200)
	firstReads := reads
	if writes != 0 {
		t.Fatal("polling wrote to the database")
	}
	for i := 0; i < 20; i++ {
		request(t, app, "alice", "POST", "/api/events", sampleEvent(), 200)
	}
	reads, writes = 0, 0
	request(t, app, "alice", "GET", "/api/state", nil, 200)
	request(t, app, "alice", "GET", "/api/events"+rangeQuery, nil, 200)
	if writes != 0 || reads != firstReads {
		t.Fatalf("query count grew: %d -> %d; writes %d", firstReads, reads, writes)
	}
}

func TestStructuredErrorsKeepContext(t *testing.T) {
	app := testApp(t)
	login(t, app, "alice")
	text, _ := settingsYAML(defaults())
	text = strings.Replace(text, "when: true", "when: {unknown: test}", 1)
	result := decode[Problem](t, request(t, app, "alice", "PUT", "/api/settings/config", map[string]any{
		"yaml": text, "poll": 15, "timezone": "UTC",
	}, 400))
	if result.Code != "unknown_rule" || result.Params["operator"] != "unknown" || result.Params["line"] == nil {
		t.Fatal("structured YAML context lost", result)
	}
	result = decode[Problem](t, request(t, app, "alice", "POST", "/api/tasks", Task{Title: "Test", Due: "2026-02-30"}, 400))
	if result.Code != "invalid_deadline" {
		t.Fatal("expected stable error code", result)
	}
	request(t, app, "alice", "PUT", "/api/settings", defaults(), 404)
}
