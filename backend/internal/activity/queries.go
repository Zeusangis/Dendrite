package activity

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

type Query struct {
	Days, Limit, Offset       int
	Domain, App, Search, Sort string
}

func (q Query) Bounded() Query {
	if q.Days < 1 {
		q.Days = 7
	}
	if q.Days > 365 {
		q.Days = 365
	}
	if q.Limit < 1 {
		q.Limit = 25
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	q.Domain = strings.Trim(strings.ToLower(q.Domain), " .")
	return q
}

type Website struct {
	NodeID        int64    `json:"node_id"`
	Type          string   `json:"type"`
	Title         string   `json:"title"`
	Domain        string   `json:"domain"`
	URL           string   `json:"url"`
	Browsers      []string `json:"browsers"`
	TotalSeconds  float64  `json:"total_seconds"`
	ActiveSeconds float64  `json:"active_seconds"`
	Sessions      int      `json:"sessions"`
	Pages         int      `json:"pages"`
	FirstSeen     string   `json:"first_seen"`
	LastSeen      string   `json:"last_seen"`
	Connections   int      `json:"connections"`
}
type WebsiteResult struct {
	Domains []Website `json:"domains,omitempty"`
	Pages   []Website `json:"pages,omitempty"`
	Total   int       `json:"total"`
	Limit   int       `json:"limit"`
	Offset  int       `json:"offset"`
}
type RecentResult struct {
	Recent []Session `json:"recent"`
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
}
type RelatedActivity struct {
	Node storage.Note `json:"node"`
	Edge storage.Edge `json:"edge"`
}
type NodeDetail struct {
	Node storage.Note `json:"node"`
	Website
	RelatedNodes []RelatedActivity `json:"related_nodes"`
	RelatedNotes []RelatedActivity `json:"related_notes"`
}

func querySessions(db *sql.DB, q Query) ([]Session, error) {
	cfg, err := readConfig(db)
	if err != nil {
		return nil, err
	}
	q = q.Bounded()
	if q.Days > cfg.RetentionDays {
		q.Days = cfg.RetentionDays
	}
	since := time.Now().AddDate(0, 0, -q.Days)
	sessions, err := loadSessions(db, since)
	if err != nil {
		return nil, err
	}
	out := []Session{}
	for _, s := range sessions {
		if s.TotalSeconds <= 0 || (q.Domain != "" && s.Domain != q.Domain && !strings.HasSuffix(s.Domain, "."+q.Domain)) || (q.App != "" && !strings.EqualFold(s.App, q.App) && !strings.EqualFold(s.AppID, q.App)) {
			continue
		}
		if q.Search != "" && !strings.Contains(strings.ToLower(s.WindowTitle+" "+s.URL+" "+s.Domain+" "+s.App), strings.ToLower(q.Search)) {
			continue
		}
		start, e := time.Parse(time.RFC3339Nano, s.StartedAt)
		end, f := time.Parse(time.RFC3339Nano, s.EndedAt)
		if e != nil || f != nil || !end.After(start) {
			continue
		}
		if start.Before(since) {
			ratio := end.Sub(since).Seconds() / end.Sub(start).Seconds()
			s.TotalSeconds *= ratio
			s.ActiveSeconds *= ratio
			s.StartedAt = since.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, s)
	}
	return out, nil
}
func bounds(length, offset, limit int) (int, int) {
	if offset > length {
		offset = length
	}
	end := offset + limit
	if end > length {
		end = length
	}
	return offset, end
}
func GetRecent(db *sql.DB, q Query) (RecentResult, error) {
	q = q.Bounded()
	sessions, err := querySessions(db, q)
	if err != nil {
		return RecentResult{}, err
	}
	sort.Slice(sessions, func(i, j int) bool {
		a, b := sessions[i], sessions[j]
		if q.Sort == "active" && a.ActiveSeconds != b.ActiveSeconds {
			return a.ActiveSeconds > b.ActiveSeconds
		}
		if q.Sort == "duration" && a.TotalSeconds != b.TotalSeconds {
			return a.TotalSeconds > b.TotalSeconds
		}
		if a.EndedAt == b.EndedAt {
			return a.ID > b.ID
		}
		return a.EndedAt > b.EndedAt
	})
	a, b := bounds(len(sessions), q.Offset, q.Limit)
	return RecentResult{sessions[a:b], len(sessions), q.Limit, q.Offset}, nil
}
func GetWebsites(db *sql.DB, typ string, q Query) (WebsiteResult, error) {
	q = q.Bounded()
	sessions, err := querySessions(db, q)
	if err != nil {
		return WebsiteResult{}, err
	}
	nodes, err := storage.ListNodes(db)
	if err != nil {
		return WebsiteResult{}, err
	}
	ids := map[string]int64{}
	for _, n := range nodes {
		ids[n.Path] = n.ID
	}
	rows, err := db.Query(`SELECT source_id,target_id FROM edges`)
	if err != nil {
		return WebsiteResult{}, err
	}
	connections := map[int64]int{}
	for rows.Next() {
		var a, b int64
		if err = rows.Scan(&a, &b); err != nil {
			rows.Close()
			return WebsiteResult{}, err
		}
		connections[a]++
		connections[b]++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return WebsiteResult{}, err
	}
	groups := map[string]*Website{}
	pages := map[string]map[string]bool{}
	browsers := map[string]map[string]bool{}
	for _, s := range sessions {
		if s.URL == "" {
			continue
		}
		key := s.Domain
		if typ == NodePage {
			key = s.URL
		}
		w := groups[key]
		if w == nil {
			w = &Website{NodeID: ids[nodeKey(typ, key)], Type: typ, Title: s.Domain, Domain: s.Domain, Browsers: []string{}, FirstSeen: s.StartedAt}
			groups[key] = w
			pages[key] = map[string]bool{}
			browsers[key] = map[string]bool{}
		}
		if typ == NodePage {
			w.URL = s.URL
			w.Title = s.WindowTitle
			if w.Title == "" {
				w.Title = s.URL
			}
		}
		w.TotalSeconds += s.TotalSeconds
		w.ActiveSeconds += s.ActiveSeconds
		w.Sessions++
		if s.StartedAt < w.FirstSeen {
			w.FirstSeen = s.StartedAt
		}
		if s.EndedAt > w.LastSeen {
			w.LastSeen = s.EndedAt
		}
		pages[key][s.URL] = true
		browsers[key][s.App] = true
	}
	list := []Website{}
	for key, w := range groups {
		for name := range browsers[key] {
			w.Browsers = append(w.Browsers, name)
		}
		sort.Strings(w.Browsers)
		w.Pages = len(pages[key])
		w.Connections = connections[w.NodeID]
		list = append(list, *w)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if q.Sort == "active" && a.ActiveSeconds != b.ActiveSeconds {
			return a.ActiveSeconds > b.ActiveSeconds
		}
		if q.Sort == "duration" && a.TotalSeconds != b.TotalSeconds {
			return a.TotalSeconds > b.TotalSeconds
		}
		if a.LastSeen != b.LastSeen {
			return a.LastSeen > b.LastSeen
		}
		return a.Domain+a.URL < b.Domain+b.URL
	})
	a, b := bounds(len(list), q.Offset, q.Limit)
	result := WebsiteResult{Total: len(list), Limit: q.Limit, Offset: q.Offset}
	if typ == NodeDomain {
		result.Domains = list[a:b]
	} else {
		result.Pages = list[a:b]
	}
	return result, nil
}
func GetNodeDetail(db *sql.DB, id int64) (*NodeDetail, error) {
	nodes, err := storage.ListNodes(db)
	if err != nil {
		return nil, err
	}
	byID := map[int64]storage.Note{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	node, ok := byID[id]
	if !ok || node.Type == "note" {
		return nil, sql.ErrNoRows
	}
	q := Query{Days: 365, Limit: 100}
	sessions, err := querySessions(db, q)
	if err != nil {
		return nil, err
	}
	d := &NodeDetail{Node: node, Website: Website{NodeID: id, Type: node.Type, Title: node.Title, Browsers: []string{}}, RelatedNodes: []RelatedActivity{}, RelatedNotes: []RelatedActivity{}}
	browsers := map[string]bool{}
	pages := map[string]bool{}
	for _, s := range sessions {
		key := ""
		switch node.Type {
		case NodeApp:
			key = s.AppID
		case NodeDomain:
			key = s.Domain
		case NodePage:
			key = s.URL
		case NodeWindow:
			if s.URL == "" {
				key = s.AppID + "|" + s.WindowTitle
			}
		}
		if key == "" || nodeKey(node.Type, key) != node.Path {
			continue
		}
		d.TotalSeconds += s.TotalSeconds
		d.ActiveSeconds += s.ActiveSeconds
		d.Sessions++
		browsers[s.App] = true
		if s.URL != "" {
			pages[s.URL] = true
		}
		if d.FirstSeen == "" || s.StartedAt < d.FirstSeen {
			d.FirstSeen = s.StartedAt
		}
		if s.EndedAt > d.LastSeen {
			d.LastSeen = s.EndedAt
		}
		if node.Type == NodeDomain || node.Type == NodePage {
			d.Domain = s.Domain
		}
		if node.Type == NodePage {
			d.URL = s.URL
		}
	}
	if d.Sessions == 0 {
		return nil, sql.ErrNoRows
	}
	for name := range browsers {
		d.Browsers = append(d.Browsers, name)
	}
	sort.Strings(d.Browsers)
	d.Pages = len(pages)
	rows, err := db.Query(`SELECT id,source_id,target_id,strength,relationship_type,source,reason,created_at FROM edges WHERE source_id=? OR target_id=? ORDER BY strength DESC,id LIMIT 100`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e storage.Edge
		if err = rows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.Strength, &e.RelationshipType, &e.Source, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		other := e.SourceID
		if other == id {
			other = e.TargetID
		}
		related := RelatedActivity{byID[other], e}
		if related.Node.Type == "note" {
			d.RelatedNotes = append(d.RelatedNotes, related)
		} else {
			d.RelatedNodes = append(d.RelatedNodes, related)
		}
		d.Connections++
	}
	return d, rows.Err()
}
func ValidateQuery(q Query) error {
	if q.Sort != "" && q.Sort != "recent" && q.Sort != "active" && q.Sort != "duration" {
		return fmt.Errorf("sort must be recent, duration, or active")
	}
	if len(q.Search) > 240 || len(q.Domain) > 253 || len(q.App) > 240 {
		return fmt.Errorf("activity filter too long")
	}
	return nil
}
