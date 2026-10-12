package activity

import (
	"database/sql"
	"encoding/json"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	NodeApp            = "app"
	NodeDomain         = "domain"
	NodePage           = "page"
	NodeWindow         = "window"
	EdgeBrowserUsage   = "browser usage"
	EdgeVisitedPage    = "visited page"
	EdgeBrowserPageUse = "browser page use"
	EdgeForeground     = "foreground context"
	EdgeContextSwitch  = "context switch"
	EdgeConcept        = "activity concept"
)

type ActivityContext struct {
	App         string
	AppID       string
	WindowTitle string
	Domain      string
	URL         string
	PageTitle   string
	Private     bool
	Focused     bool
}

func browserIdentity(app, id string) (string, string) {
	for _, b := range []struct {
		name, id string
		aliases  []string
	}{
		{"Google Chrome", "com.google.Chrome", []string{"chrome", "google chrome", "com.google.chrome"}},
		{"Brave Browser", "com.brave.Browser", []string{"brave", "brave browser", "com.brave.browser"}},
		{"Microsoft Edge", "com.microsoft.edgemac", []string{"edge", "microsoft edge", "com.microsoft.edgemac"}},
		{"Safari", "com.apple.Safari", []string{"safari", "com.apple.safari"}},
		{"Firefox", "org.mozilla.firefox", []string{"firefox", "org.mozilla.firefox"}},
		{"Arc", "company.thebrowser.Browser", []string{"arc", "company.thebrowser.browser"}},
	} {
		for _, alias := range b.aliases {
			if strings.EqualFold(app, alias) || strings.EqualFold(id, alias) {
				return b.name, b.id
			}
		}
	}
	if id == "" {
		id = app
	}
	return app, id
}

func sameBrowser(app, id, hintApp string) bool {
	_, a := browserIdentity(app, id)
	_, b := browserIdentity(hintApp, "")
	return strings.EqualFold(a, b)
}

// SanitizeURL never accepts a caller-supplied domain as evidence. Paths are not
// collapsed (trailing slashes can distinguish resources); only empty roots are
// canonicalized. Raw paths may still contain sensitive information.
func SanitizeURL(raw string) (string, string) {
	if len(raw) > 2048 || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Opaque != "" {
		return "", ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if (u.Scheme != "http" && u.Scheme != "https") || host == "" || len(host) > 253 || strings.ContainsAny(host, " %/\\?#") {
		return "", ""
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", ""
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", ""
		}
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = nil, "", "", "", false
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), host
}

func NormalizeContext(ctx ActivityContext, cfg Config) (ActivityContext, bool) {
	if ctx.Private || !ctx.Focused || len(ctx.App) > 240 || len(ctx.AppID) > 240 || len(ctx.WindowTitle) > 1000 || len(ctx.PageTitle) > 1000 || len(ctx.URL) > 2048 {
		return ActivityContext{}, false
	}
	originalApp, originalID := strings.TrimSpace(ctx.App), strings.TrimSpace(ctx.AppID)
	ctx.App, ctx.AppID = browserIdentity(originalApp, originalID)
	if ctx.App == "" {
		return ActivityContext{}, false
	}
	for _, app := range cfg.ExcludedApps {
		app = strings.TrimSpace(app)
		_, excludedID := browserIdentity(app, "")
		if app != "" && (strings.EqualFold(excludedID, ctx.AppID) || strings.EqualFold(app, originalApp) || strings.EqualFold(app, originalID) || strings.EqualFold(app, ctx.App) || strings.EqualFold(app, ctx.AppID)) {
			return ActivityContext{}, false
		}
	}
	title := strings.ToLower(ctx.WindowTitle + " " + ctx.PageTitle)
	if strings.Contains(title, "incognito") || strings.Contains(title, "private browsing") {
		return ActivityContext{}, false
	}
	ctx.URL, ctx.Domain = SanitizeURL(ctx.URL)
	for _, domain := range cfg.ExcludedDomains {
		domain = strings.Trim(strings.ToLower(domain), " .")
		if domain != "" && (ctx.Domain == domain || strings.HasSuffix(ctx.Domain, "."+domain)) {
			return ActivityContext{}, false
		}
	}
	if !cfg.BrowserPages {
		ctx.URL, ctx.Domain = "", ""
	}
	if !cfg.WindowTitles {
		ctx.WindowTitle, ctx.PageTitle = "", ""
	}
	return ctx, true
}

func normalizeSample(s Sample, cfg Config) (Sample, bool) {
	if s.Locked || math.IsNaN(s.IdleSeconds) || math.IsInf(s.IdleSeconds, 0) || s.IdleSeconds < 0 {
		return s, false
	}
	ctx, ok := NormalizeContext(ActivityContext{App: s.App, AppID: s.AppID, WindowTitle: s.WindowTitle, URL: s.URL, Private: s.Private, Focused: true}, cfg)
	s.App, s.AppID, s.WindowTitle, s.URL, s.Domain = ctx.App, ctx.AppID, ctx.WindowTitle, ctx.URL, ctx.Domain
	return s, ok
}

func readConfig(db *sql.DB) (Config, error) {
	cfg := DefaultConfig()
	var raw string
	err := db.QueryRow(`SELECT config_json FROM activity_settings WHERE id=1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var disk diskConfig
	err = json.Unmarshal([]byte(raw), &disk)
	return disk.Config, err
}

// ReconcilePrivacy removes excluded/expired history and sanitizes legacy rows.
// Disabling metadata removes retained metadata too, not just its projections.
func ReconcilePrivacy(db *sql.DB, cfg Config, now time.Time) error {
	rows, err := db.Query(`SELECT id,app,app_id,window_title,url,ended_at FROM activity_sessions`)
	if err != nil {
		return err
	}
	type update struct {
		id   int64
		ctx  ActivityContext
		keep bool
	}
	updates := []update{}
	cutoff := now.AddDate(0, 0, -cfg.RetentionDays)
	for rows.Next() {
		var id int64
		var app, appID, title, raw, end string
		if err = rows.Scan(&id, &app, &appID, &title, &raw, &end); err != nil {
			rows.Close()
			return err
		}
		ctx, ok := NormalizeContext(ActivityContext{App: app, AppID: appID, WindowTitle: title, URL: raw, Focused: true}, cfg)
		ended, e := time.Parse(time.RFC3339Nano, end)
		updates = append(updates, update{id, ctx, ok && e == nil && !ended.Before(cutoff)})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, u := range updates {
		if !u.keep {
			// Removing a suppressed context must not bridge two surviving sessions
			// into an invented transition, even when their wall-clock gap is short.
			_, err = tx.Exec(`UPDATE activity_sessions SET run_id='' WHERE run_id<>'' AND run_id=(SELECT run_id FROM activity_sessions WHERE id=?)`, u.id)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`DELETE FROM activity_sessions WHERE id=?`, u.id)
		} else {
			_, err = tx.Exec(`UPDATE activity_sessions SET app=?,app_id=?,window_title=?,url=?,domain=? WHERE id=? AND (app<>? OR app_id<>? OR window_title<>? OR url<>? OR domain<>?)`, u.ctx.App, u.ctx.AppID, u.ctx.WindowTitle, u.ctx.URL, u.ctx.Domain, u.id, u.ctx.App, u.ctx.AppID, u.ctx.WindowTitle, u.ctx.URL, u.ctx.Domain)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func observedSwitch(previous, current Session) bool {
	if previous.RunID == "" || previous.RunID != current.RunID || previous.ActiveSeconds <= 0 || current.ActiveSeconds <= 0 || previous.TotalSeconds <= 0 || current.TotalSeconds <= 0 || previous.AppID == current.AppID {
		return false
	}
	end, e := time.Parse(time.RFC3339Nano, previous.EndedAt)
	start, s := time.Parse(time.RFC3339Nano, current.StartedAt)
	gap := start.Sub(end)
	return e == nil && s == nil && gap >= 0 && gap <= 30*time.Second
}
