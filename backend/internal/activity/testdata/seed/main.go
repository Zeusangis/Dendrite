package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeusangis/dendrite/internal/storage"
)

func main() {
	if len(os.Args) != 2 || !strings.HasPrefix(filepath.Base(os.Args[1]), "dendrite-e2e-") {
		panic("only temporary e2e roots are allowed")
	}
	db, err := storage.OpenDatabase(filepath.Join(os.Args[1], "data", "dendrite.db"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		panic(err)
	}
	defer tx.Rollback()
	at := time.Now().Add(-10 * time.Minute)
	for i, p := range []struct{ title, url string }{
		{"Go programming language", "https://en.wikipedia.org/wiki/Go_(programming_language)?secret=x#token"},
		{"Go tutorial", "https://www.geeksforgeeks.org/go-language/"},
		{"Go source", "https://github.com/golang/go"},
		{"Go programming language", "https://go.dev/"},
		{"Excluded fixture", "https://private.example/secret"},
	} {
		start := at.Add(time.Duration(i) * time.Minute)
		if _, err = tx.Exec(`INSERT INTO activity_sessions(app,app_id,window_title,domain,url,started_at,ended_at,total_seconds,active_seconds,samples,run_id) VALUES('Google Chrome','com.google.Chrome',?,'',?,?,?,30,25,6,'fixture')`, p.title, p.url, start.UTC().Format(time.RFC3339Nano), start.Add(30*time.Second).UTC().Format(time.RFC3339Nano)); err != nil {
			panic(err)
		}
	}
	if err = tx.Commit(); err != nil {
		panic(err)
	}
	fmt.Println("Synthetic website fixture seeded")
}
