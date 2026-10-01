package activity

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/relationships"
	"github.com/zeusangis/dendrite/internal/storage"
)

type Session struct {
	ID            int64   `json:"id"`
	App           string  `json:"app"`
	AppID         string  `json:"app_id"`
	WindowTitle   string  `json:"window_title"`
	Domain        string  `json:"domain"`
	URL           string  `json:"url"`
	StartedAt     string  `json:"started_at"`
	EndedAt       string  `json:"ended_at"`
	TotalSeconds  float64 `json:"total_seconds"`
	ActiveSeconds float64 `json:"active_seconds"`
	Samples       int     `json:"samples"`
	RunID         string  `json:"-"`
}
type Usage struct {
	NodeID        int64   `json:"node_id"`
	Title         string  `json:"title"`
	Type          string  `json:"type"`
	TotalSeconds  float64 `json:"total_seconds"`
	ActiveSeconds float64 `json:"active_seconds"`
	Sessions      int     `json:"sessions"`
	LastSeen      string  `json:"last_seen"`
}
type Summary struct {
	TotalSeconds  float64   `json:"total_seconds"`
	ActiveSeconds float64   `json:"active_seconds"`
	ActiveRatio   float64   `json:"active_ratio"`
	Usage         []Usage   `json:"usage"`
	Recent        []Session `json:"recent"`
}

func loadSessions(db *sql.DB, since time.Time) ([]Session, error) {
	rows, err := db.Query(`SELECT id,app,app_id,window_title,domain,url,started_at,ended_at,total_seconds,active_seconds,samples,run_id FROM activity_sessions WHERE ended_at>=? ORDER BY started_at,id`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []Session{}
	for rows.Next() {
		var s Session
		if err = rows.Scan(&s.ID, &s.App, &s.AppID, &s.WindowTitle, &s.Domain, &s.URL, &s.StartedAt, &s.EndedAt, &s.TotalSeconds, &s.ActiveSeconds, &s.Samples, &s.RunID); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
func GetSummary(db *sql.DB, days int) (Summary, error) {
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	since := time.Now().AddDate(0, 0, -days)
	sessions, err := loadSessions(db, since)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Usage: []Usage{}, Recent: []Session{}}
	byApp := map[string]*Usage{}
	for _, s := range sessions {
		start, _ := time.Parse(time.RFC3339Nano, s.StartedAt)
		end, _ := time.Parse(time.RFC3339Nano, s.EndedAt)
		if start.Before(since) && end.After(since) && end.After(start) {
			factor := end.Sub(since).Seconds() / end.Sub(start).Seconds()
			s.TotalSeconds *= factor
			s.ActiveSeconds *= factor
		}
		summary.TotalSeconds += s.TotalSeconds
		summary.ActiveSeconds += s.ActiveSeconds
		key := s.AppID
		if key == "" {
			key = s.App
		}
		u := byApp[key]
		if u == nil {
			u = &Usage{Title: s.App, Type: "app"}
			n, err := storage.GetNodeByPath(db, nodeKey("app", key))
			if err != nil {
				return summary, err
			}
			if n != nil {
				u.NodeID = n.ID
			}
			byApp[key] = u
		}
		u.TotalSeconds += s.TotalSeconds
		u.ActiveSeconds += s.ActiveSeconds
		u.Sessions++
		u.LastSeen = s.EndedAt
	}
	if summary.TotalSeconds > 0 {
		summary.ActiveRatio = summary.ActiveSeconds / summary.TotalSeconds
	}
	for _, u := range byApp {
		summary.Usage = append(summary.Usage, *u)
	}
	sort.Slice(summary.Usage, func(i, j int) bool { return summary.Usage[i].ActiveSeconds > summary.Usage[j].ActiveSeconds })
	for i := len(sessions) - 1; i >= 0 && len(summary.Recent) < 50; i-- {
		summary.Recent = append(summary.Recent, sessions[i])
	}
	return summary, nil
}
func NodeUsage(db *sql.DB, id int64) (*Usage, error) {
	u := &Usage{NodeID: id}
	err := db.QueryRow(`SELECT n.title,n.type,s.total_seconds,s.active_seconds,s.sessions,s.last_seen FROM activity_node_stats s JOIN nodes n ON n.id=s.node_id WHERE s.node_id=?`, id).Scan(&u.Title, &u.Type, &u.TotalSeconds, &u.ActiveSeconds, &u.Sessions, &u.LastSeen)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}
func nodeKey(typ, key string) string {
	return fmt.Sprintf("activity:%s:%x", typ, sha256.Sum256([]byte(key)))
}

type derived struct {
	Title, Type, Key string
	Total, Active    float64
	Sessions         int
	First, Last      time.Time
	ID               int64
}
type evidence struct {
	A, B    string
	Seconds float64
	Count   int
	Kind    string
}

// Strength combines active exposure, repetition and active fraction; bounded
// monotonic inputs make the automatic score inspectable instead of arbitrary.
func Strength(active, total float64, count int, transition bool) float64 {
	score := 20 + math.Min(60, math.Log1p(math.Max(0, active)/60)*12) + math.Min(35, math.Log1p(float64(count))*10)
	if total > 0 {
		score += math.Min(20, 20*math.Max(0, active)/total)
	}
	if transition {
		score = 15 + math.Min(90, math.Log1p(float64(count))*25) + math.Min(25, math.Log1p(math.Max(0, active)/60)*6)
	}
	return math.Round(math.Min(150, score)*100) / 100
}
func Rebuild(db *sql.DB) error {
	// Retention applies while paused too, because graph sync keeps running.
	var configJSON string
	configErr := db.QueryRow(`SELECT config_json FROM activity_settings WHERE id=1`).Scan(&configJSON)
	if configErr != nil && configErr != sql.ErrNoRows {
		return configErr
	}
	if configErr == nil {
		var config diskConfig
		if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
			return err
		}
		if config.RetentionDays > 0 {
			if _, err := db.Exec(`DELETE FROM activity_sessions WHERE ended_at<?`, time.Now().AddDate(0, 0, -config.RetentionDays).UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
	}
	sessions, err := loadSessions(db, time.Time{})
	if err != nil {
		return err
	}
	derivedNodes := map[string]*derived{}
	links := map[string]*evidence{}
	addNode := func(typ, key, title string, s Session) string {
		path := nodeKey(typ, key)
		n := derivedNodes[path]
		first, _ := time.Parse(time.RFC3339Nano, s.StartedAt)
		last, _ := time.Parse(time.RFC3339Nano, s.EndedAt)
		if n == nil {
			n = &derived{Title: title, Type: typ, Key: path, First: first}
			derivedNodes[path] = n
		}
		n.Total += s.TotalSeconds
		n.Active += s.ActiveSeconds
		n.Sessions++
		n.Last = last
		return path
	}
	addEdge := func(a, b, kind string, seconds float64) {
		if a == b {
			return
		}
		if a > b {
			a, b = b, a
		}
		key := a + "|" + b
		v := links[key]
		if v == nil {
			v = &evidence{A: a, B: b, Kind: kind}
			links[key] = v
		}
		v.Seconds += seconds
		v.Count++
	}
	var previous *Session
	previousApp := ""
	for i, s := range sessions {
		if s.TotalSeconds <= 0 {
			previous = nil
			continue
		}
		appID := s.AppID
		if appID == "" {
			appID = s.App
		}
		app := addNode("app", appID, s.App, s)
		if s.WindowTitle != "" && s.URL == "" {
			window := addNode("window", appID+"|"+s.WindowTitle, s.WindowTitle, s)
			addEdge(app, window, "foreground context", s.ActiveSeconds)
		}
		if s.Domain != "" {
			domain := addNode("domain", s.Domain, s.Domain, s)
			addEdge(app, domain, "browser usage", s.ActiveSeconds)
			if s.URL != "" {
				title := s.WindowTitle
				if title == "" {
					title = s.URL
				}
				page := addNode("page", s.URL, title, s)
				addEdge(domain, page, "visited page", s.ActiveSeconds)
				addEdge(app, page, "browser page use", s.ActiveSeconds)
			}
		}
		if previous != nil && previous.ActiveSeconds > 0 && s.ActiveSeconds > 0 && previousApp != app && previous.RunID == s.RunID {
			end, _ := time.Parse(time.RFC3339Nano, previous.EndedAt)
			start, _ := time.Parse(time.RFC3339Nano, s.StartedAt)
			gap := start.Sub(end)
			if gap >= 0 && gap <= 30*time.Second {
				addEdge(previousApp, app, "context switch", math.Min(previous.ActiveSeconds, s.ActiveSeconds))
			}
		}
		previous = &sessions[i]
		previousApp = app
	}
	// Upsert activity projections; preserve positions, delete expired evidence.
	all, err := storage.ListNodes(db)
	if err != nil {
		return err
	}
	for _, n := range all {
		if n.Type == "app" || n.Type == "window" || n.Type == "domain" || n.Type == "page" {
			if _, ok := derivedNodes[n.Path]; !ok {
				if err = storage.DeleteNodeByPath(db, n.Path); err != nil {
					return err
				}
			}
		}
	}
	keys := []string{}
	for key := range derivedNodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		n := derivedNodes[key]
		id, err := storage.UpsertNode(db, n.Key, n.Title, n.Type, n.First, n.Last)
		if err != nil {
			return err
		}
		n.ID = id
		if _, err = db.Exec(`INSERT INTO activity_node_stats(node_id,total_seconds,active_seconds,sessions,last_seen) VALUES(?,?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET total_seconds=excluded.total_seconds,active_seconds=excluded.active_seconds,sessions=excluded.sessions,last_seen=excluded.last_seen`, id, n.Total, n.Active, n.Sessions, n.Last.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	edges := []storage.Edge{}
	for _, v := range links {
		a, b := derivedNodes[v.A], derivedNodes[v.B]
		total := math.Min(a.Total, b.Total)
		strength := Strength(v.Seconds, total, v.Count, v.Kind == "context switch")
		reason := fmt.Sprintf("%s; %.1f active minutes; %d observed sessions", v.Kind, v.Seconds/60, v.Count)
		if total > 0 {
			reason += fmt.Sprintf("; %.0f%% active", math.Min(1, v.Seconds/total)*100)
		}
		idA, idB := a.ID, b.ID
		if idA > idB {
			idA, idB = idB, idA
		}
		edges = append(edges, storage.Edge{SourceID: idA, TargetID: idB, Strength: strength, RelationshipType: v.Kind, Source: "activity", Reason: reason})
	}
	// Link note concepts only when a real title/tag/entity occurs in an activity
	// context. App proximity alone is not evidence of a shared subject.
	noteNodes, err := storage.ListNotes(db)
	if err != nil {
		return err
	}
	for _, note := range noteNodes {
		concepts := []string{note.Title}
		rows, err := db.Query(`SELECT tag FROM note_tags WHERE note_id=? UNION SELECT name FROM entities WHERE note_id=?`, note.ID, note.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var concept string
			if err = rows.Scan(&concept); err != nil {
				rows.Close()
				return err
			}
			concepts = append(concepts, concept)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, node := range derivedNodes {
			if node.Type == "app" || node.Active <= 0 {
				continue
			}
			matched := ""
			for _, concept := range concepts {
				if len([]rune(concept)) >= 3 && phraseMatch(node.Title, concept) {
					matched = concept
					break
				}
			}
			if matched == "" {
				continue
			}
			a, b := note.ID, node.ID
			if a > b {
				a, b = b, a
			}
			edges = append(edges, storage.Edge{SourceID: a, TargetID: b, Strength: math.Min(150, Strength(node.Active, node.Total, node.Sessions, false)+20), RelationshipType: "activity concept", Source: "activity", Reason: fmt.Sprintf("Shared note concept: %s; %.1f active minutes; %d sessions", matched, node.Active/60, node.Sessions)})
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS activity_pairs(a INTEGER,b INTEGER,PRIMARY KEY(a,b));DELETE FROM activity_pairs;`); err != nil {
		return err
	}
	for _, e := range edges {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO activity_pairs VALUES(?,?)`, e.SourceID, e.TargetID); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO edges(source_id,target_id,strength,relationship_type,source,reason,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source_id,target_id) DO UPDATE SET strength=excluded.strength,relationship_type=excluded.relationship_type,source=excluded.source,reason=excluded.reason`, e.SourceID, e.TargetID, e.Strength, e.RelationshipType, e.Source, e.Reason, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DELETE FROM edges WHERE source='activity' AND NOT EXISTS(SELECT 1 FROM activity_pairs p WHERE p.a=edges.source_id AND p.b=edges.target_id)`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	snap, err := storage.GetGraph(db)
	if err != nil {
		return err
	}
	threshold := 25.0
	value, err := storage.GetSetting(db, "min_auto_edge_strength")
	if err != nil {
		return err
	}
	if value != "" {
		fmt.Sscan(value, &threshold)
	}
	visible := []storage.Edge{}
	for _, e := range snap.Edges {
		if e.Source == "explicit" || e.Strength >= threshold {
			visible = append(visible, e)
		}
	}
	for id, score := range relationships.ComputeImportance(snap.Nodes, visible, nil) {
		if err = storage.UpdateImportance(db, id, score); err != nil {
			return err
		}
	}
	return nil
}
func phraseMatch(text, phrase string) bool {
	normalized := func(s string) string {
		return " " + strings.Join(strings.Fields(strings.ToLower(strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127 {
				return r
			}
			return ' '
		}, s))), " ") + " "
	}
	return strings.Contains(normalized(text), normalized(phrase))
}
