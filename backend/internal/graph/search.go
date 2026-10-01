package graph

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/zeusangis/dendrite/internal/storage"
)

type SearchHit struct {
	Node    storage.Note `json:"node"`
	Snippet string       `json:"snippet,omitempty"`
	MatchIn string       `json:"match_in"`
	Score   float64      `json:"score"`
}
type SearchFilters struct {
	Tag     string
	Project string
	Path    string
}

func (s *Service) Search(q string, limit int) ([]SearchHit, error) {
	return s.SearchFiltered(q, limit, SearchFilters{})
}
func (s *Service) SearchFiltered(q string, limit int, filters SearchFilters) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	hits := []SearchHit{}
	q = strings.TrimSpace(q)
	if q == "" && filters.Tag == "" && filters.Project == "" && filters.Path == "" {
		return hits, nil
	}
	query := `SELECT n.id,n.type,n.title,n.path,n.created_at,n.updated_at,n.importance,n.x,n.y,substr(f.content,1,180),f.tags,f.entities FROM note_search f JOIN nodes n ON n.id=f.rowid WHERE 1=1`
	args := []interface{}{}
	match := ""
	if q != "" {
		// Quoted queries match a phrase; otherwise all terms must match with prefixes.
		if strings.HasPrefix(q, `"`) && strings.HasSuffix(q, `"`) && len(q) > 2 {
			match = `"` + strings.ReplaceAll(q[1:len(q)-1], `"`, `""`) + `"`
		} else {
			terms := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
			if len(terms) == 0 {
				return hits, nil
			}
			for _, t := range terms {
				if match != "" {
					match += " AND "
				}
				match += `"` + t + `"*`
			}
		}
		query += ` AND note_search MATCH ?`
		args = append(args, match)
	}
	if filters.Tag != "" {
		query += ` AND EXISTS(SELECT 1 FROM note_tags t WHERE t.note_id=n.id AND t.tag=? COLLATE NOCASE)`
		args = append(args, strings.TrimPrefix(filters.Tag, "#"))
	}
	if filters.Project != "" {
		query += ` AND f.project=? COLLATE NOCASE`
		args = append(args, filters.Project)
	}
	if filters.Path != "" {
		query += ` AND substr(n.path,1,length(?))=?`
		args = append(args, filters.Path, filters.Path)
	}
	if q != "" {
		query += ` ORDER BY bm25(note_search,8,1,4,3,2,0),n.title`
	} else {
		query += ` ORDER BY n.title`
	}
	query += ` LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n storage.Note
		var snippet, tags, entities string
		if err = rows.Scan(&n.ID, &n.Type, &n.Title, &n.Path, &n.CreatedAt, &n.UpdatedAt, &n.Importance, &n.X, &n.Y, &snippet, &tags, &entities); err != nil {
			return nil, err
		}
		where, score := "content", 20.0
		lc := strings.ToLower(strings.Trim(q, `"`))
		if q != "" && strings.Contains(strings.ToLower(n.Title), lc) {
			where, score = "title", 100
		} else if q != "" && strings.Contains(strings.ToLower(tags), lc) {
			where, score = "tag", 40
		} else if q != "" && strings.Contains(strings.ToLower(entities), lc) {
			where, score = "entity", 30
		}
		hits = append(hits, SearchHit{n, snippet, where, score})
	}
	return hits, rows.Err()
}
