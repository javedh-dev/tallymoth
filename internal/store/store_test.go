package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func count(t *testing.T, d *DB) (n int) {
	t.Helper()
	if err := d.sql.QueryRow("SELECT count(*) FROM events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return
}

func TestWriteCapsAndPrune(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	old := now - 100*msPerDay
	if err := d.Accept([]Event{{now, "a", "{}"}, {old, "a", "{}"}}, 3, 100); err != nil {
		t.Fatal(err)
	}
	if err := d.Accept([]Event{{now, "a", "{}"}, {now, "a", "{}"}}, 3, 100); err != ErrCap {
		t.Fatalf("want ErrCap, got %v", err) // 2 + 2 > 3
	}
	if err := d.Accept([]Event{{now, "a", "{}"}}, 3, 0); err != ErrFull {
		t.Fatalf("want ErrFull, got %v", err)
	}
	if err := d.Close(); err != nil { // Close flushes the queue
		t.Fatal(err)
	}

	d, _ = Open(dir) // reopen: daily counter is rebuilt from the data
	defer d.Close()
	if n := count(t, d); n != 2 {
		t.Fatalf("want 2 stored events, got %d", n)
	}
	if err := d.Accept([]Event{{now, "a", "{}"}, {now, "a", "{}"}}, 2, 100); err != ErrCap {
		t.Fatalf("counter should survive restart, got %v", err)
	}
	if err := d.Prune(90); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d); n != 1 {
		t.Fatalf("prune should delete the 100-day-old event, have %d", n)
	}
	var av int
	d.sql.QueryRow("PRAGMA auto_vacuum").Scan(&av)
	if av != 2 {
		t.Errorf("auto_vacuum=%d, want 2 (incremental)", av)
	}
}

// Upgrade safety: old DBs migrate with data intact, a copy is kept, and a newer DB is refused.
func TestMigrations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.db")

	// Fixture: a DB at schema v1, written by raw SQL (stands in for "created by an older release").
	raw, _ := sql.Open("sqlite", path)
	raw.SetMaxOpenConns(1)
	files, _ := migrationFS.ReadFile("migrations/0001_init.sql")
	raw.Exec(string(files))
	raw.Exec("INSERT INTO events (ts,name,props) VALUES (1,'old','{}')")
	raw.Exec("PRAGMA user_version = 1")
	raw.Close()

	d, err := Open(dir) // current binary is v1 => no-op, data intact
	if err != nil {
		t.Fatal(err)
	}
	if count(t, d) != 1 {
		t.Fatal("existing data lost")
	}
	var v int
	d.sql.QueryRow("PRAGMA user_version").Scan(&v)
	if v != SchemaVersion() {
		t.Fatalf("user_version=%d want %d", v, SchemaVersion())
	}
	d.Close()

	// Fresh dir: migrates from 0 and a second open is a no-op.
	d2, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()

	// Newer than the binary: refuse, leave the file alone.
	raw, _ = sql.Open("sqlite", path)
	raw.Exec("PRAGMA user_version = 999")
	raw.Close()
	if _, err := Open(dir); err == nil {
		t.Fatal("a newer schema must be refused")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("refused DB must stay in place")
	}
}

func TestAcceptAfterClose(t *testing.T) {
	d, _ := Open(t.TempDir())
	d.Close()
	if err := d.Accept([]Event{{1, "a", "{}"}}, 10, 100); err != ErrClosed { // must not panic on the closed channel
		t.Fatalf("want ErrClosed, got %v", err)
	}
}
