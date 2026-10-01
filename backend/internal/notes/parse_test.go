package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseContent(t *testing.T) {
	md := `---
project: dendrite
status: active
---

# Go Interfaces

Interfaces define behavior in [[Go]].

I'm learning interfaces while working on [[TaskCLI|my CLI]].

Tags: #golang #backend #Go

Code: ` + "`http.Server`" + ` and ` + "`read_config`" + `.

## Heading #should-not-count

CamelCase ident: TaskCLI. Phrase: Backend Engineering.`
	// NOTE: raw backtick concatenation above; simpler to write directly:
	md = strings.ReplaceAll(md, "` + \"`\" + `", "`")

	p, err := parseContent(md, "notes/go/interfaces.md")
	if err != nil {
		t.Fatal(err)
	}

	if p.Title != "Go Interfaces" {
		t.Errorf("title = %q, want %q", p.Title, "Go Interfaces")
	}
	if p.Project != "dendrite" {
		t.Errorf("project = %q, want %q", p.Project, "dendrite")
	}
	wantLinks := []string{"Go", "TaskCLI"}
	if len(p.Links) != len(wantLinks) {
		t.Fatalf("links = %v, want %v", p.Links, wantLinks)
	}
	for i, w := range wantLinks {
		if p.Links[i] != w {
			t.Errorf("links[%d] = %q, want %q", i, p.Links[i], w)
		}
	}

	wantTags := map[string]bool{"golang": true, "backend": true, "go": true}
	if len(p.Tags) != len(wantTags) {
		t.Fatalf("tags = %v, want exactly %v", p.Tags, wantTags)
	}
	for _, tag := range p.Tags {
		if !wantTags[tag] {
			t.Errorf("unexpected tag %q", tag)
		}
	}

	// Front matter values.
	if p.Entities == nil {
		t.Fatal("entities nil")
	}
	names := map[string]bool{}
	for _, e := range p.Entities {
		names[e.Name] = true
	}
	for _, want := range []string{"Go", "TaskCLI", "golang", "Backend Engineering", "http.Server", "read_config"} {
		if !names[want] {
			t.Errorf("missing entity %q; got %v", want, names)
		}
	}
}

func TestExtractLinksDedupAndAlias(t *testing.T) {
	got := extractLinks("[[A]] [[a]] [[B|alias]] [[B]] [[ C D ]]")
	want := []string{"A", "B", "C D"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestTagsSkipHeadings(t *testing.T) {
	body := "# Heading #notag\n\nSome text #real-tag\n( paren #paren-tag\n"
	got := extractTags(body)
	if len(got) != 2 || got[0] != "real-tag" || got[1] != "paren-tag" {
		t.Fatalf("tags = %v, want [real-tag paren-tag]", got)
	}
}

func TestTitleToFilename(t *testing.T) {
	cases := map[string]string{
		"Go Interfaces":      "go-interfaces",
		"  Weird__Title!!  ": "weird-title",
		"A -- B // C":        "a-b-c",
		"Über Café":          "über-café",
	}
	for in, want := range cases {
		if got := TitleToFilename(in); got != want {
			t.Errorf("TitleToFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseNoteOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.md")
	if err := os.WriteFile(path, []byte("# Hello\n\nLink to [[World]].\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ParseNote(path, "sample.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Hello" || len(p.Links) != 1 || p.Links[0] != "World" {
		t.Fatalf("unexpected parse: %+v", p)
	}
}
