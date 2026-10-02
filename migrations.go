package main

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

// Convert the old per-member permissions and separate fallback color once.
func migrateLegacy(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var events []struct {
			ID, Creator, Source string
			Members             string
		}
		if err := tx.Table("events").Where("COALESCE(edit_policy, '') = ''").Find(&events).Error; err != nil {
			return err
		}
		for _, event := range events {
			var members []struct {
				User   string
				Editor bool
			}
			if err := json.Unmarshal([]byte(event.Members), &members); err != nil {
				return err
			}
			if event.Creator == "" && len(members) > 0 {
				event.Creator = members[0].User
			}
			all, author := true, true
			for _, member := range members {
				all = all && member.Editor
				author = author && (member.Editor == (member.User == event.Creator))
			}
			policy := "author"
			if event.Source == "" {
				if all {
					policy = "all"
				} else if !author {
					return fmt.Errorf("event %s has legacy permissions that require a manual choice", event.ID)
				}
			}
			if err := tx.Table("events").Where("id = ?", event.ID).Updates(map[string]any{
				"creator": event.Creator, "edit_policy": policy,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`UPDATE sources SET error = CASE WHEN error = '' THEN NULL
      ELSE json_object('code', 'invalid_value', 'params', json_object('detail', error)) END
      WHERE error IS NOT NULL AND NOT json_valid(error)`).Error; err != nil {
			return err
		}
		// JSON is kept only here for compatibility with databases made before YAML settings.
		return tx.Exec(`UPDATE users SET settings = json_remove(
      CASE WHEN COALESCE(json_extract(settings, '$.colors[#-1].rule.op'), '') = 'true'
      THEN settings ELSE json_insert(settings, '$.colors[#]', json_object(
        'rule', json_object('op', 'true'),
        'color', COALESCE(json_extract(settings, '$.color'), 'teal')))
      END, '$.color') WHERE json_type(settings, '$.color') IS NOT NULL`).Error
	})
}
