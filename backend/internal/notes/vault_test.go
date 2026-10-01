package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultPathsAndExclusiveCreate(t *testing.T) {
	v := &Vault{Root: t.TempDir()}
	for _, path := range []string{"../outside.md", "sub/../../outside.md", "/absolute.md", "file.txt", ".private.md", "sub/./file.md", "sub\\file.md", ""} {
		if _, err := v.AbsPath(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if err := v.WriteRaw("nested/note.md", "# Note", true); err != nil {
		t.Fatal(err)
	}
	if err := v.WriteRaw("nested/note.md", "lost", true); err != ErrConflict {
		t.Fatal("create overwrite", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(v.Root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.AbsPath("escape/file.md"); err == nil {
		t.Fatal("symlink traversal accepted")
	}
}
func TestTitleAndFrontMatterFidelity(t *testing.T) {
	raw := "---\r\nproject: 'lab'\r\n---\r\n\r\n## Section\r\n```md\r\n# Code heading\r\n[[Not a link]] #notag\r\n```\r\n# Old title\r\n\r\nUnchanged text\r\n---\r\nTrailing text"
	updated := WithTitle(raw, "New title")
	want := strings.Replace(raw, "# Old title", "# New title", 1)
	if updated != want {
		t.Fatalf("content changed beyond title: %q", updated)
	}
	pn, err := parseContent(updated, "note.md")
	if err != nil {
		t.Fatal(err)
	}
	if pn.Title != "New title" || pn.Project != "lab" || len(pn.Links) != 0 || len(pn.Tags) != 0 {
		t.Fatalf("parse %+v", pn)
	}
	malformed := "---\nproject: lab\nno closing delimiter"
	body, _ := stripFrontMatter(malformed)
	if body != malformed {
		t.Fatal("malformed front matter lost")
	}
	long := "---\nproject: lab\n---\n# Title\n" + strings.Repeat("x", 100000) + "\n"
	body, _ = stripFrontMatter(long)
	if !strings.HasSuffix(body, strings.Repeat("x", 100000)+"\n") {
		t.Fatal("long line truncated")
	}
}
