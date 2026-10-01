package notes

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// wikiLinkRe matches [[wikilinks]], including alias form [[Target|alias]].
var wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]|]+)(?:\|[^\[\]]*)?\]\]`)

// tagRe matches #tags inside a line's content, never headings.
var tagRe = regexp.MustCompile(`(?:^|[\s(#{\[])"*#([A-Za-z][\w/-]*)`)

var yamlKeyRe = regexp.MustCompile(`^([A-Za-z_-]+)\s*:\s*(.*)$`)
var listItemRe = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)
var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// ParsedNote holds everything extracted from one Markdown file.
type ParsedNote struct {
	RelPath  string
	Title    string
	Project  string
	Body     string
	Links    []string
	Tags     []string
	Entities []Entity
}

// Entity is a named concept extracted from note text.
type Entity struct {
	Name   string
	Kind   string
	Weight float64
}

// ParseNote reads and parses one Markdown file.
func ParseNote(absPath, relPath string) (*ParsedNote, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	return parseContent(string(data), relPath)
}

// ParseContent parses the exact supplied snapshot without a second disk read.
func ParseContent(content, relPath string) (*ParsedNote, error) {
	return parseContent(content, relPath)
}

// parseContent parses Markdown text from memory (used by tests too).
func parseContent(content, relPath string) (*ParsedNote, error) {
	body, front := stripFrontMatter(content)

	title := extractTitle(body)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}

	prose := withoutFences(body)
	links := extractLinks(prose)
	tags := extractTags(prose)
	ents := extractEntities(body, title, links, tags)

	return &ParsedNote{
		RelPath:  relPath,
		Title:    title,
		Project:  front["project"],
		Body:     body,
		Links:    links,
		Tags:     tags,
		Entities: ents,
	}, nil
}

// stripFrontMatter removes a leading YAML front matter block and returns the
// remaining body plus parsed simple key: value pairs.
func stripFrontMatter(content string) (string, map[string]string) {
	front := map[string]string{}
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return content, front
	}
	lines := strings.SplitAfterN(content, "\n", 2)
	if len(lines) < 2 {
		return content, front
	}
	rest := lines[1]
	offset := len(lines[0])
	for _, chunk := range strings.SplitAfter(rest, "\n") {
		line := strings.TrimSuffix(strings.TrimSuffix(chunk, "\n"), "\r")
		offset += len(chunk)
		if line == "---" {
			return content[offset:], front
		}
		if m := yamlKeyRe.FindStringSubmatch(line); m != nil {
			front[strings.ToLower(m[1])] = strings.Trim(strings.TrimSpace(m[2]), `"'`)
		}
	}
	return content, map[string]string{}
}

// extractTitle prefers the first H1 heading.
func extractTitle(body string) string {
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

// extractLinks returns de-duplicated [[wikilink]] targets in order.
func extractLinks(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range wikiLinkRe.FindAllStringSubmatch(body, -1) {
		t := normalizeTitle(m[1])
		key := strings.ToLower(t)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

// extractTags returns de-duplicated #tags (skipping Markdown headings).
func extractTags(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if headingRe.MatchString(line) {
			continue
		}
		for _, m := range tagRe.FindAllStringSubmatch(line, -1) {
			t := strings.ToLower(m[1])
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// extractEntities pulls candidate concepts from wikilinks, tags, code spans
// and capitalized multi-word phrases.
func extractEntities(body, title string, links, tags []string) []Entity {
	seen := map[string]bool{}
	var out []Entity
	add := func(name, kind string, w float64) {
		name = strings.TrimSpace(name)
		if name == "" || len(name) < 2 || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, Entity{Name: name, Kind: kind, Weight: w})
	}

	for _, l := range links {
		add(l, "link", 1.0)
	}
	for _, t := range tags {
		add(t, "tag", 0.8)
	}
	add(title, "title", 0.9)

	for _, line := range strings.Split(body, "\n") {
		// Inline code spans: `TaskCLI`
		for _, m := range codeSpanRe.FindAllStringSubmatch(line, -1) {
			add(m[1], "code", 0.7)
		}
		// CamelCase / snake_case identifiers and Capitalized Phrases.
		for _, m := range identRe.FindAllStringSubmatch(line, -1) {
			name, kind := m[1], "phrase"
			if name == "" {
				name, kind = m[2], "ident"
			}
			if name == "" {
				name, kind = m[3], "ident"
			}
			add(name, kind, 0.5)
		}
	}
	return out
}

var codeSpanRe = regexp.MustCompile("`([^`]+)`")
var identRe = regexp.MustCompile(`\b([A-Z][a-z]+(?:\s+[A-Z][a-z]+)+)\b|\b([A-Z][a-z0-9]*[A-Z][A-Za-z0-9]*)\b|\b([a-z]+_[a-z0-9_]+)\b`)

func withoutFences(body string) string {
	var out strings.Builder
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// normalizeTitle canonicalizes a link target or note title.
func normalizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "#")
	return strings.Join(strings.Fields(s), " ")
}

// TitleToFilename converts a note title into a safe file name.
func TitleToFilename(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := true // suppress leading dashes
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// StatTimes returns (modTime, birthTime-ish creation time) for a file.
// Creation falls back to the mod time on filesystems without btime.
func StatTimes(path string) (time.Time, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Now(), time.Now()
	}
	return info.ModTime(), info.ModTime()
}
