package api

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/activity"
)

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
		writeJSON(w, map[string]bool{"ok": true})
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
		writeErr(w, 404, "unknown activity endpoint")
	}
}
