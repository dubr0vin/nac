package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type Task struct {
	ID          string   `json:"id"`
	UserID      string   `json:"-" gorm:"index"`
	Version     int      `json:"version"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Due         string   `json:"due"`
	Completed   bool     `json:"completed"`
	Tags        []string `json:"tags" gorm:"serializer:json"`
	Color       string   `json:"color" gorm:"-"`
	Stripe      string   `json:"stripe,omitempty" gorm:"-"`
}

func (a *App) putTask(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var task Task
	if err := c.Bind(&task); err != nil {
		return err
	}
	task.ID, task.UserID = c.Param("id"), currentUser(c).ID
	if task.ID != "" {
		var old Task
		if err := a.db.First(&old, "id = ? AND user_id = ?", task.ID, task.UserID).Error; err != nil {
			return echo.ErrNotFound
		}
		if task.Version != old.Version {
			return echo.NewHTTPError(409, problem("task_conflict"))
		}
	} else {
		task.ID, task.Version = newID(), 0
	}
	task.Title = strings.TrimSpace(task.Title)
	if task.Title == "" || len(task.Title) > 1000 || len(task.Description) > 65536 {
		return echo.NewHTTPError(400, problem("invalid_task"))
	}
	if task.Due != "" {
		if _, err := time.Parse("2006-01-02", task.Due); err != nil {
			return echo.NewHTTPError(400, problem("invalid_deadline"))
		}
	}
	if err := validateTags(task.Tags, false); err != nil {
		return badRequest(err)
	}
	task.Tags = unique(task.Tags)
	task.Version++
	if err := a.db.Save(&task).Error; err != nil {
		return err
	}
	return c.JSON(200, task)
}

func (a *App) deleteTask(c echo.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var task Task
	if err := a.db.First(&task, "id = ? AND user_id = ?", c.Param("id"), currentUser(c).ID).Error; err != nil {
		return echo.ErrNotFound
	}
	if c.QueryParam("version") != fmt.Sprint(task.Version) {
		return echo.NewHTTPError(409, problem("task_conflict"))
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Event{}).Where("task_id = ?", task.ID).Update("task_id", "").Error; err != nil {
			return err
		}
		return tx.Delete(&task).Error
	})
	if err != nil {
		return err
	}
	return c.JSON(200, echo.Map{})
}
