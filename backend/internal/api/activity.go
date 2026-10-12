package api

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/activity"
)

func activityQuery(w http.ResponseWriter, r *http.Request) (activity.Query, bool) {
	v := r.URL.Query()
	q := activity.Query{Domain: v.Get("domain"), App: v.Get("app"), Search: v.Get("search"), Sort: v.Get("sort")}
	for name, target := range map[string]*int{"days": &q.Days, "limit": &q.Limit, "offset": &q.Offset} {
		if raw := v.Get(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				writeErr(w, 400, "invalid "+name)
				return q, false
			}
			*target = n
		}
	}
	if err := activity.ValidateQuery(q); err != nil {
		writeErr(w, 400, err.Error())
		return q, false
	}
	return q.Bounded(), true
}

func (h *Handler) ActivityHandler(w http.ResponseWriter, r *http.Request) {
	if h.Activity == nil {
		writeErr(w, 503, "activity collector unavailable")
		return
	}
	sub := strings.TrimPrefix(r.URL.Path, "/api/activity")
	switch sub {
	case "":
		if method(w, r, "GET") {
			writeJSON(w, h.Activity.Status())
		}
	case "/config":
		if !method(w, r, "PUT") {
			return
		}
		var cfg activity.Config
		if !decode(w, r, &cfg) {
			return
		}
		if err := h.Activity.Configure(cfg); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		if err := h.Service.RebuildLocked(); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, h.Activity.Status())
	case "/series":
		if !method(w, r, "GET") {
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		granularity := r.URL.Query().Get("granularity")
		if granularity != "" && granularity != "day" && granularity != "week" {
			writeErr(w, 400, "granularity must be day or week")
			return
		}
		series, err := activity.GetSeries(h.DB, days, granularity, time.Now())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, series)
	case "/summary":
		if !method(w, r, "GET") {
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		summary, err := activity.GetSummary(h.DB, days)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, summary)
	case "/recent", "/domains", "/pages":
		if !method(w, r, "GET") {
			return
		}
		q, ok := activityQuery(w, r)
		if !ok {
			return
		}
		if sub == "/recent" {
			result, err := activity.GetRecent(h.DB, q)
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, result)
		} else {
			typ := activity.NodeDomain
			if sub == "/pages" {
				typ = activity.NodePage
			}
			result, err := activity.GetWebsites(h.DB, typ, q)
			if err != nil {
				fail(w, err)
				return
			}
			items := result.Domains
			if typ == activity.NodePage {
				items = result.Pages
			}
			writeJSON(w, map[string]interface{}{strings.TrimPrefix(sub, "/"): items, "total": result.Total, "limit": result.Limit, "offset": result.Offset})
		}
	case "/pairing":
		if method(w, r, "GET") {
			writeJSON(w, map[string]string{"token": h.Activity.Token()})
		}
	case "/browser":
		if !method(w, r, "POST") {
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(h.Activity.Token())) != 1 {
			writeErr(w, 401, "browser pairing token required")
			return
		}
		var hint activity.BrowserHint
		if !decode(w, r, &hint) {
			return
		}
		if len(hint.App) > 240 || len(hint.URL) > 2048 || len(hint.Title) > 1000 {
			writeErr(w, 400, "browser metadata too long")
			return
		}
		h.Activity.Browser(hint)
		state := h.Activity.Status()
		writeJSON(w, map[string]bool{"ok": true, "recording": state.Config.Enabled && state.Config.BrowserPages})
	case "/data":
		if !method(w, r, "DELETE") {
			return
		}
		if r.URL.Query().Get("confirm") != "true" {
			writeErr(w, 400, "confirm=true is required")
			return
		}
		if err := h.Activity.Clear(); err != nil {
			fail(w, err)
			return
		}
		if err := h.Service.RebuildLocked(); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		if strings.HasPrefix(sub, "/nodes/") {
			if !method(w, r, "GET") {
				return
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(sub, "/nodes/"), 10, 64)
			if err != nil || id <= 0 {
				writeErr(w, 400, "invalid activity node id")
				return
			}
			detail, err := activity.GetNodeDetail(h.DB, id)
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, detail)
			return
		}
		writeErr(w, 404, "unknown activity endpoint")
	}
}
