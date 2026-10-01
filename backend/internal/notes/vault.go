package notes

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Vault struct{ Root string }
type NoteFile struct {
	RelPath string
	ModTime int64
	Size    int64
}

var ErrNotFound = errors.New("note not found")
var ErrConflict = errors.New("note changed or already exists; reload before saving")

func (v *Vault) List() ([]NoteFile, error) {
	out := []NoteFile{}
	err := filepath.WalkDir(v.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != v.Root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in vault: %s", path)
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(v.Root, path)
		if err != nil {
			return err
		}
		out = append(out, NoteFile{filepath.ToSlash(rel), info.ModTime().Unix(), info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, err
}

func (v *Vault) Raw(path string) (string, error) {
	abs, err := v.AbsPath(path)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		return "", ErrNotFound
	}
	return string(b), err
}
func (v *Vault) Read(path string) (*ParsedNote, error) {
	raw, err := v.Raw(path)
	if err != nil {
		return nil, err
	}
	return parseContent(raw, path)
}
func Revision(raw string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) }

// WithTitle replaces the first real H1, preserving front matter and all other text.
func WithTitle(raw, title string) string {
	title = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(title, "\r", " "), "\n", " "))
	if title == "" {
		return raw
	}
	body, _ := stripFrontMatter(raw)
	prefix := raw[:len(raw)-len(body)]
	lines := strings.SplitAfter(body, "\n")
	fenced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(line, "# ") {
			ending := ""
			if strings.HasSuffix(line, "\n") {
				ending = "\n"
				if strings.HasSuffix(line, "\r\n") {
					ending = "\r\n"
				}
			}
			lines[i] = "# " + title + ending
			return prefix + strings.Join(lines, "")
		}
	}
	return prefix + "# " + title + "\n\n" + body
}

func (v *Vault) Write(path, title, body string) (string, error) {
	if path == "" {
		slug := TitleToFilename(title)
		if slug == "" {
			return "", fmt.Errorf("valid title required")
		}
		path = slug + ".md"
	}
	return path, v.WriteRaw(path, WithTitle(body, title), false)
}

// WriteRaw uses an atomic rename for updates and an exclusive link for creates.
func (v *Vault) WriteRaw(path, raw string, exclusive bool) error {
	abs, err := v.AbsPath(path)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".dendrite-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	mode := os.FileMode(0644)
	if info, e := os.Stat(abs); e == nil {
		mode = info.Mode().Perm()
	}
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.WriteString(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if exclusive {
		err = os.Link(tmp.Name(), abs)
		if os.IsExist(err) {
			return ErrConflict
		}
		return err
	}
	return os.Rename(tmp.Name(), abs)
}
func (v *Vault) Delete(path string) error {
	abs, err := v.AbsPath(path)
	if err != nil {
		return err
	}
	err = os.Remove(abs)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}
func (v *Vault) AbsPath(path string) (string, error) {
	if path == "" || strings.Contains(path, "\\") || filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".md") {
		return "", fmt.Errorf("invalid Markdown path: %s", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." || part == "." || part == "" || strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("invalid note path: %s", path)
		}
	}
	root, err := filepath.Abs(v.Root)
	if err != nil {
		return "", err
	}
	// Reject existing symlink components, including the root itself.
	current := root
	for _, part := range append([]string{""}, strings.Split(path, "/")...) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink paths are not allowed")
		}
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}
