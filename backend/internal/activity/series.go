package activity

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

const TopSeriesApps = 5

type AppBucket struct {
	App           string  `json:"app"`
	AppID         string  `json:"app_id"`
	TotalSeconds  float64 `json:"total_seconds"`
	ActiveSeconds float64 `json:"active_seconds"`
}

type TimeBucket struct {
	Start           string      `json:"start"`
	Label           string      `json:"label"`
	TotalSeconds    float64     `json:"total_seconds"`
	ActiveSeconds   float64     `json:"active_seconds"`
	IdleSeconds     float64     `json:"idle_seconds"`
	ContextSwitches int         `json:"context_switches"`
	Apps            []AppBucket `json:"apps"`
}

type Series struct {
	Granularity string       `json:"granularity"`
	Days        int          `json:"days"`
	Apps        []string     `json:"apps"`
	AppIDs      []string     `json:"app_ids"`
	Buckets     []TimeBucket `json:"buckets"`
}

type appTotals struct {
	name   string
	total  float64
	active float64
}
type bucketWork struct {
	data TimeBucket
	apps map[string]*appTotals
}

// GetSeries groups local activity into calendar days or Monday-starting ISO
// weeks. Durations crossing bucket edges are split; active time is apportioned
// by the recorded session ratio. The range contains days calendar dates,
// including the current partial day (or week).
func GetSeries(db *sql.DB, days int, granularity string, now time.Time) (Series, error) {
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	if granularity == "" {
		granularity = "day"
	}
	if granularity != "day" && granularity != "week" {
		return Series{}, fmt.Errorf("granularity must be day or week")
	}

	loc := now.Location()
	localNow := now.In(loc)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	rangeStart := today.AddDate(0, 0, -(days - 1))
	first := rangeStart
	if granularity == "week" {
		fromMonday := (int(rangeStart.Weekday()) + 6) % 7
		first = rangeStart.AddDate(0, 0, -fromMonday)
	}
	count := days
	if granularity == "week" {
		count = 0
		for week := first; !week.After(today); week = week.AddDate(0, 0, 7) {
			count++
		}
	}

	starts := make([]time.Time, count+1)
	for i := range starts {
		if granularity == "day" {
			starts[i] = first.AddDate(0, 0, i)
		} else {
			starts[i] = first.AddDate(0, 0, i*7)
		}
	}
	works := make([]bucketWork, count)
	for i := range works {
		start := starts[i]
		label := start.Format("Mon Jan 2")
		if granularity == "week" {
			end := starts[i+1].AddDate(0, 0, -1)
			label = start.Format("Jan 2") + "–" + end.Format("Jan 2")
		}
		works[i] = bucketWork{
			data: TimeBucket{Start: start.Format(time.RFC3339), Label: label, Apps: []AppBucket{}},
			apps: map[string]*appTotals{},
		}
	}
	periodEnd := localNow
	if starts[count].Before(periodEnd) {
		periodEnd = starts[count]
	}
	sessions, err := loadSessions(db, rangeStart.Add(-30*time.Second))
	if err != nil {
		return Series{}, err
	}

	for _, session := range sessions {
		start, startErr := time.Parse(time.RFC3339Nano, session.StartedAt)
		end, endErr := time.Parse(time.RFC3339Nano, session.EndedAt)
		if startErr != nil || endErr != nil || !end.After(start) || session.TotalSeconds <= 0 {
			continue
		}
		if start.Before(rangeStart) {
			start = rangeStart
		}
		if end.After(periodEnd) {
			end = periodEnd
		}
		if !end.After(start) {
			continue
		}
		appID := session.AppID
		if appID == "" {
			appID = session.App
		}
		for cursor := start; cursor.Before(end); {
			index := sort.Search(len(starts)-1, func(i int) bool { return starts[i+1].After(cursor) })
			if index >= len(works) {
				break
			}
			segmentEnd := end
			if starts[index+1].Before(segmentEnd) {
				segmentEnd = starts[index+1]
			}
			seconds := segmentEnd.Sub(cursor).Seconds()
			activeRatio := math.Max(0, math.Min(1, session.ActiveSeconds/session.TotalSeconds))
			active := seconds * activeRatio
			work := &works[index]
			work.data.TotalSeconds += seconds
			work.data.ActiveSeconds += active
			app := work.apps[appID]
			if app == nil {
				name := session.App
				if name == "" {
					name = appID
				}
				app = &appTotals{name: name}
				work.apps[appID] = app
			}
			app.total += seconds
			app.active += active
			cursor = segmentEnd
		}
	}

	// Count only observed, active app changes in one collector run. Pauses,
	// idle-only sessions, restart boundaries, and long gaps are not switches.
	var previous *Session
	previousApp := ""
	for i := range sessions {
		session := &sessions[i]
		if session.TotalSeconds <= 0 || session.ActiveSeconds <= 0 {
			previous = nil
			continue
		}
		appID := session.AppID
		if appID == "" {
			appID = session.App
		}
		if previous != nil && previous.RunID == session.RunID && previousApp != appID {
			previousEnd, endErr := time.Parse(time.RFC3339Nano, previous.EndedAt)
			start, startErr := time.Parse(time.RFC3339Nano, session.StartedAt)
			gap := start.Sub(previousEnd)
			if endErr == nil && startErr == nil && gap >= 0 && gap <= 30*time.Second && !start.Before(rangeStart) && start.Before(periodEnd) {
				index := sort.Search(len(starts)-1, func(i int) bool { return starts[i+1].After(start) })
				if index < len(works) {
					works[index].data.ContextSwitches++
				}
			}
		}
		previous, previousApp = session, appID
	}

	totals := map[string]*appTotals{}
	for i := range works {
		work := &works[i]
		work.data.IdleSeconds = math.Max(0, work.data.TotalSeconds-work.data.ActiveSeconds)
		for id, app := range work.apps {
			total := totals[id]
			if total == nil {
				total = &appTotals{name: app.name}
				totals[id] = total
			}
			total.total += app.total
			total.active += app.active
		}
	}
	appIDs := make([]string, 0, len(totals))
	for id := range totals {
		appIDs = append(appIDs, id)
	}
	sort.Slice(appIDs, func(i, j int) bool {
		a, b := totals[appIDs[i]], totals[appIDs[j]]
		if a.total == b.total {
			if a.name == b.name {
				return appIDs[i] < appIDs[j]
			}
			return a.name < b.name
		}
		return a.total > b.total
	})
	if len(appIDs) > TopSeriesApps {
		appIDs = appIDs[:TopSeriesApps]
	}
	series := Series{Granularity: granularity, Days: days, Apps: []string{}, AppIDs: []string{}, Buckets: make([]TimeBucket, len(works))}
	for _, id := range appIDs {
		series.Apps = append(series.Apps, totals[id].name)
		series.AppIDs = append(series.AppIDs, id)
	}
	for i := range works {
		work := &works[i]
		other := AppBucket{App: "Other", AppID: "other"}
		for _, id := range appIDs {
			if app := work.apps[id]; app != nil {
				work.data.Apps = append(work.data.Apps, AppBucket{App: app.name, AppID: id, TotalSeconds: app.total, ActiveSeconds: app.active})
			}
		}
		for id, app := range work.apps {
			if !contains(appIDs, id) {
				other.TotalSeconds += app.total
				other.ActiveSeconds += app.active
			}
		}
		if other.TotalSeconds > 0 {
			work.data.Apps = append(work.data.Apps, other)
		}
		series.Buckets[i] = work.data
	}
	return series, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
