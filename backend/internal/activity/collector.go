package activity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled         bool     `json:"enabled"`
	WindowTitles    bool     `json:"window_titles"`
	BrowserPages    bool     `json:"browser_pages"`
	IdleSeconds     int      `json:"idle_seconds"`
	RetentionDays   int      `json:"retention_days"`
	ExcludedApps    []string `json:"excluded_apps"`
	ExcludedDomains []string `json:"excluded_domains"`
	Token           string   `json:"-"`
}
type diskConfig struct {
	Config
	Token string `json:"token"`
}

func DefaultConfig() Config {
	return Config{Enabled: true, WindowTitles: true, BrowserPages: true, IdleSeconds: 60, RetentionDays: 30, ExcludedApps: []string{"1Password", "Bitwarden", "Keychain Access"}, ExcludedDomains: []string{}}
}

type Sample struct {
	App         string  `json:"app"`
	AppID       string  `json:"app_id"`
	WindowTitle string  `json:"window_title"`
	URL         string  `json:"url"`
	Domain      string  `json:"domain"`
	IdleSeconds float64 `json:"idle_seconds"`
	Locked      bool    `json:"locked"`
	Private     bool    `json:"private"`
	Warning     string  `json:"warning,omitempty"`
}
type Sampler interface {
	Sample(context.Context, Config) (Sample, error)
}
type Locker interface {
	Lock()
	Unlock()
}
type Collector struct {
	db         *sql.DB
	gate       Locker
	sampler    Sampler
	mu         sync.Mutex
	config     Config
	last       *Sample
	lastAt     time.Time
	sessionID  int64
	runID      string
	warning    string
	lastSample string
	browser    *BrowserHint
	lastPrune  time.Time
	generation uint64
}
type BrowserHint struct {
	App     string    `json:"app"`
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	Private bool      `json:"private"`
	Focused bool      `json:"focused"`
	At      time.Time `json:"-"`
}
type Status struct {
	Config           Config `json:"config"`
	Supported        bool   `json:"supported"`
	Warning          string `json:"warning"`
	LastSample       string `json:"last_sample"`
	BrowserConnected bool   `json:"browser_connected"`
}

func New(db *sql.DB, gate Locker, sampler Sampler, autostart bool) (*Collector, error) {
	cfg := DefaultConfig()
	cfg.Enabled = autostart
	var raw string
	err := db.QueryRow(`SELECT config_json FROM activity_settings WHERE id=1`).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		var disk diskConfig
		if err = json.Unmarshal([]byte(raw), &disk); err != nil {
			return nil, err
		}
		cfg = disk.Config
		cfg.Token = disk.Token
	}
	if cfg.Token == "" {
		cfg.Token = randomToken()
	}
	c := &Collector{db: db, gate: gate, sampler: sampler, config: cfg, runID: randomToken()}
	return c, c.persist(cfg)
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (c *Collector) persist(cfg Config) error {
	b, err := json.Marshal(diskConfig{Config: cfg, Token: cfg.Token})
	if err != nil {
		return err
	}
	_, err = c.db.Exec(`INSERT INTO activity_settings(id,config_json) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET config_json=excluded.config_json`, string(b))
	return err
}
func (c *Collector) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.config
	cfg.ExcludedApps = append([]string{}, cfg.ExcludedApps...)
	cfg.ExcludedDomains = append([]string{}, cfg.ExcludedDomains...)
	return Status{cfg, PlatformSupported(), c.warning, c.lastSample, c.browser != nil && time.Since(c.browser.At) < 40*time.Second}
}
func (c *Collector) Token() string { c.mu.Lock(); defer c.mu.Unlock(); return c.config.Token }
func (c *Collector) Configure(cfg Config) error {
	if cfg.IdleSeconds < 15 || cfg.IdleSeconds > 900 || cfg.RetentionDays < 1 || cfg.RetentionDays > 365 || len(cfg.ExcludedApps) > 100 || len(cfg.ExcludedDomains) > 100 {
		return fmt.Errorf("idle threshold must be 15–900 seconds; retention 1–365 days; at most 100 exclusions")
	}
	for _, value := range append(append([]string{}, cfg.ExcludedApps...), cfg.ExcludedDomains...) {
		if len(value) > 200 {
			return fmt.Errorf("exclusion is too long")
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg.Token = c.config.Token
	if err := c.persist(cfg); err != nil {
		return err
	}
	if _, err := c.db.Exec(`DELETE FROM activity_sessions WHERE ended_at<?`, time.Now().AddDate(0, 0, -cfg.RetentionDays).UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	c.lastPrune = time.Now()
	c.config = cfg
	c.generation++
	c.runID = randomToken()
	c.last = nil
	c.sessionID = 0
	c.browser = nil
	c.lastAt = time.Time{}
	return nil
}
func (c *Collector) Browser(hint BrowserHint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.config.Enabled || !c.config.BrowserPages {
		return
	}
	hint.At = time.Now()
	// Keep suppression hints so native metadata cannot bypass extension exclusions.
	if !hint.Focused {
		hint.Private = true
	}

	c.browser = &hint
}
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			config, generation := c.config, c.generation
			c.mu.Unlock()
			if !config.Enabled {
				continue
			}
			sampleCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			sample, err := c.sampler.Sample(sampleCtx, config)
			cancel()
			c.gate.Lock()
			c.mu.Lock()
			stale := generation != c.generation
			c.mu.Unlock()
			if stale {
				c.gate.Unlock()
				continue
			}
			if err != nil {
				c.mu.Lock()
				c.warning = err.Error()
				c.last = nil
				c.sessionID = 0
				c.runID = randomToken()
				c.mu.Unlock()
			} else {
				if err = c.Observe(sample, time.Now()); err != nil {
					c.mu.Lock()
					c.warning = err.Error()
					c.mu.Unlock()
				}
			}
			c.gate.Unlock()
		}
	}
}
func (c *Collector) filter(s Sample) (Sample, bool) {
	if s.App == "" || s.Locked || s.Private || math.IsNaN(s.IdleSeconds) || math.IsInf(s.IdleSeconds, 0) || s.IdleSeconds < 0 {
		return s, false
	}
	for _, app := range c.config.ExcludedApps {
		if strings.TrimSpace(app) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(app), s.App) || strings.EqualFold(strings.TrimSpace(app), s.AppID) {
			return s, false
		}
	}
	lowerTitle := strings.ToLower(s.WindowTitle)
	if strings.Contains(lowerTitle, "incognito") || strings.Contains(lowerTitle, "private browsing") {
		return s, false
	}
	if !c.config.WindowTitles {
		s.WindowTitle = ""
	}
	if !c.config.BrowserPages {
		s.URL = ""
		s.Domain = ""
	} else if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			s.URL = ""
			s.Domain = ""
		} else {
			u.User = nil
			u.RawQuery = ""
			u.Fragment = ""
			s.Domain = strings.ToLower(u.Hostname())
			s.URL = u.String()
		}
	}
	for _, domain := range c.config.ExcludedDomains {
		domain = strings.Trim(strings.ToLower(domain), " .")
		if domain != "" && (s.Domain == domain || strings.HasSuffix(s.Domain, "."+domain)) {
			return s, false
		}
	}
	if len(s.App) > 240 || len(s.AppID) > 240 || len(s.WindowTitle) > 1000 || len(s.URL) > 2048 {
		return s, false
	}
	return s, true
}

// Observe assigns each bounded interval to the previously observed foreground
// context. Gaps (sleep/errors/pause) are discarded, never billed as active use.
func (c *Collector) Observe(sample Sample, at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.config.Enabled {
		c.last = nil
		c.sessionID = 0
		return nil
	}
	if c.browser != nil && at.Sub(c.browser.At) >= 0 && at.Sub(c.browser.At) < 40*time.Second && strings.EqualFold(sample.App, c.browser.App) {
		if c.browser.Private {
			sample.Private = true
		}
		sample.URL = c.browser.URL
		if c.config.WindowTitles {
			sample.WindowTitle = c.browser.Title
		}
	}
	sample, ok := c.filter(sample)
	c.warning = sample.Warning
	c.lastSample = at.UTC().Format(time.RFC3339)
	if !ok {
		c.runID = randomToken()
		c.last = nil
		c.sessionID = 0
		c.lastAt = at
		return nil
	}
	elapsed := at.Sub(c.lastAt).Seconds()
	continuous := c.last != nil && elapsed > 0 && elapsed <= 15
	if c.last != nil && !continuous {
		c.runID = randomToken()
	}
	if continuous {
		active := 0.0
		if sample.IdleSeconds < float64(c.config.IdleSeconds) && c.last.IdleSeconds < float64(c.config.IdleSeconds) {
			active = elapsed
		} else if c.last.IdleSeconds < float64(c.config.IdleSeconds) {
			active = math.Max(0, math.Min(elapsed, float64(c.config.IdleSeconds)-c.last.IdleSeconds))
		}
		if c.sessionID == 0 {
			result, err := c.db.Exec(`INSERT INTO activity_sessions(app,app_id,window_title,domain,url,started_at,ended_at,total_seconds,active_seconds,samples,run_id) VALUES(?,?,?,?,?,?,?,?,?,1,?)`, c.last.App, c.last.AppID, c.last.WindowTitle, c.last.Domain, c.last.URL, c.lastAt.UTC().Format(time.RFC3339Nano), at.UTC().Format(time.RFC3339Nano), elapsed, active, c.runID)
			if err != nil {
				return err
			}
			c.sessionID, err = result.LastInsertId()
			if err != nil {
				return err
			}
		} else {
			if _, err := c.db.Exec(`UPDATE activity_sessions SET ended_at=?,total_seconds=total_seconds+?,active_seconds=active_seconds+?,samples=samples+1 WHERE id=?`, at.UTC().Format(time.RFC3339Nano), elapsed, active, c.sessionID); err != nil {
				return err
			}
		}
	}
	if !continuous || c.last.AppID != sample.AppID || c.last.App != sample.App || c.last.WindowTitle != sample.WindowTitle || c.last.URL != sample.URL {
		c.sessionID = 0
	}
	if c.lastAt.IsZero() {
		c.lastAt = at
	}
	copy := sample
	c.last = &copy
	c.lastAt = at
	if at.Sub(c.lastPrune) > time.Hour {
		if _, err := c.db.Exec(`DELETE FROM activity_sessions WHERE ended_at<?`, at.AddDate(0, 0, -c.config.RetentionDays).UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		c.lastPrune = at
	}
	return nil
}
func (c *Collector) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = nil
	c.sessionID = 0
	c.browser = nil
	c.generation++
	c.runID = randomToken()
	_, err := c.db.Exec(`DELETE FROM activity_sessions; DELETE FROM edges WHERE source='activity'; DELETE FROM nodes WHERE type IN ('app','window','domain','page');`)
	return err
}

var ErrUnsupported = errors.New("native foreground tracking is currently supported on macOS only")
