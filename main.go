package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"
)

//go:embed web/dist
var assets embed.FS

type App struct {
	db          *gorm.DB
	mu          sync.Mutex
	sourceLocks sync.Map
	trusted     []netip.Prefix
	devUser     string
	client      *http.Client
}

func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func currentUser(c echo.Context) User { return c.Get("user").(User) }
func badRequest(err error) error      { return echo.NewHTTPError(400, asProblem(err)) }

func (a *App) authenticate(c echo.Context) error {
	r := c.Request()
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	addr, _ := netip.ParseAddr(host)
	trusted := slices.ContainsFunc(a.trusted, func(p netip.Prefix) bool { return p.Contains(addr.Unmap()) })
	subject := r.Header.Get("Remote-Sub")
	login := r.Header.Get("Remote-User")
	name := r.Header.Get("Remote-Name")
	if a.devUser != "" && addr.IsLoopback() {
		subject, login, name = "dev:"+a.devUser, a.devUser, a.devUser
		trusted = true
	}
	if !trusted || subject == "" {
		return echo.NewHTTPError(401, problem("proxy_required"))
	}
	if len(subject) > 1024 || len(login) > 200 || len(name) > 200 {
		return echo.NewHTTPError(400, problem("identity_too_long"))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var user User
	err := a.db.Where(User{Subject: subject}).
		Attrs(User{ID: newID(), Settings: defaults()}).FirstOrCreate(&user).Error
	if err == nil && (user.Login != login || user.Name != name) {
		user.Login, user.Name = login, name
		err = a.db.Model(&user).Select("Login", "Name").Updates(user).Error
	}
	if err == nil {
		c.Set("user", user)
	}
	return err
}

func (a *App) auth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if err := a.authenticate(c); err != nil {
			return err
		}
		if c.Request().Method != "GET" && c.Request().Header.Get("X-NAC") != "1" {
			return echo.NewHTTPError(403, problem("csrf_header_required"))
		}
		return next(c)
	}
}

func (a *App) handler() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = handleError
	e.Use(middleware.Recover(), middleware.BodyLimit("16M"))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("Cache-Control", "no-store")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Content-Type-Options", "nosniff")
			if c.Request().URL.Path != "/view" {
				h.Set("Content-Security-Policy", "frame-ancestors 'none'")
			}
			return next(c)
		}
	})
	api := e.Group("/api", a.auth)
	api.GET("/state", a.state)
	api.POST("/tasks", a.putTask)
	api.PUT("/tasks/:id", a.putTask)
	api.DELETE("/tasks/:id", a.deleteTask)
	api.PUT("/settings/config", a.putPreferences)
	api.GET("/events", a.listEvents)
	api.POST("/availability", a.availability)
	api.GET("/events/:id", a.getEvent)
	api.POST("/events", a.putEvent)
	api.PUT("/events/:id", a.putEvent)
	api.DELETE("/events/:id", a.deleteEvent)
	api.PUT("/events/:id/tags", a.putTags)
	api.POST("/sources", a.putSource)
	api.PUT("/sources/:id", a.putSource)
	api.DELETE("/sources/:id", a.deleteSource)
	api.POST("/sources/:id/token", a.sourceToken)
	api.DELETE("/sources/:id/token", a.sourceToken)
	api.POST("/sources/:id/refresh", a.refreshSource)
	api.POST("/exports", a.putExport)
	api.PUT("/exports/:id", a.putExport)
	api.PUT("/exports/:id/kiosk", a.putKiosk)
	api.DELETE("/exports/:id", a.deleteExport)
	api.POST("/exports/:id/rotate", a.rotateExport)
	// Push tokens and export UUIDs authenticate these endpoints independently.
	e.POST("/api/sources/:id/import", a.push)
	e.GET("/export.ics", a.exportICS)
	e.GET("/view/data", a.viewData)
	dist, _ := fs.Sub(assets, "web/dist")
	e.FileFS("/", "index.html", dist)
	e.FileFS("/view", "index.html", dist)
	e.FileFS("/event", "index.html", dist)
	e.StaticFS("/assets", echo.MustSubFS(dist, "assets"))
	return e
}

func (a *App) state(c echo.Context) error {
	user := currentUser(c)
	users := []User{}
	if err := a.db.Select("id", "login", "name").Order("name").Find(&users).Error; err != nil {
		return err
	}
	sources, err := a.sources(user.ID)
	if err != nil {
		return err
	}
	exports, err := a.exports(user.ID)
	if err != nil {
		return err
	}
	events, err := a.events(user.ID)
	if err != nil {
		return err
	}
	tasks := []Task{}
	if err := a.db.Where("user_id = ?", user.ID).Order("rowid").Find(&tasks).Error; err != nil {
		return err
	}
	for i := range tasks {
		color := user.Settings.EventColor(tasks[i].Tags)
		tasks[i].Color, tasks[i].Stripe = color.Color, color.Stripe
	}
	states, err := a.tagStates(user.ID)
	if err != nil {
		return err
	}
	tags := slices.Clone(user.Settings.Tags)
	for _, event := range events {
		state := states[event.ID]
		tags = append(tags, event.Categories...)
		for _, override := range event.Overrides {
			tags = append(tags, override.Categories...)
		}
		for _, personal := range state {
			tags = append(tags, personal.Add...)
		}
	}
	user.Settings.Config, err = settingsYAML(user.Settings)
	if err != nil {
		return err
	}
	return c.JSON(200, echo.Map{"me": user, "users": users, "sources": sources, "exports": exports, "tags": unique(tags), "tasks": tasks})
}

func window(c echo.Context) (time.Time, time.Time, error) {
	from, err := time.Parse(time.RFC3339, c.QueryParam("from"))
	if err != nil {
		return from, time.Time{}, echo.NewHTTPError(400, problem("invalid_range_start"))
	}
	to, err := time.Parse(time.RFC3339, c.QueryParam("to"))
	if err != nil || !to.After(from) || to.Sub(from) > 370*24*time.Hour {
		return from, to, echo.NewHTTPError(400, problem("invalid_range"))
	}
	return from, to, nil
}

func run() error {
	path := os.Getenv("NAC_DB")
	if path == "" {
		path = "data/nac.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	db, err := openDB(path)
	if err != nil {
		return err
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	a := &App{db: db, devUser: os.Getenv("NAC_DEV_USER"), client: &http.Client{Timeout: 30 * time.Second}}
	proxies := os.Getenv("NAC_TRUSTED_PROXIES")
	if proxies == "" {
		proxies = "127.0.0.1/32,::1/128"
	}
	for _, subnet := range strings.Split(proxies, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(subnet))
		if err != nil {
			return err
		}
		a.trusted = append(a.trusted, prefix)
	}
	addr := os.Getenv("NAC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if a.devUser != "" {
		log.Print("development identity enabled for loopback connections")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go a.scheduler(ctx)
	server := &http.Server{
		Addr: addr, Handler: a.handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 40 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("NAC listening on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
