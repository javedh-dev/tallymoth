// Package store keeps one SQLite file per tenant. Writes are queued and group-committed by one goroutine.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

var (
	ErrCap    = errors.New("daily cap reached")
	ErrFull   = errors.New("database size cap reached")
	ErrBusy   = errors.New("write queue full")
	ErrClosed = errors.New("tenant closed")
)

const (
	flushEvery = 250 * time.Millisecond
	flushAt    = 500  // events
	queueSize  = 1024 // batches waiting for the writer
	pruneChunk = 5000
	msPerDay   = 86_400_000
)

type Event struct {
	TS    int64 // unix ms
	Name  string
	Props string // JSON object
}

type DB struct {
	sql  *sql.DB
	path string
	in   chan []Event
	done chan struct{}

	mu     sync.Mutex // guards the daily counter and closed
	closed bool
	day    int64
	today  int

	bytes atomic.Int64 // db + wal size after the last commit
}

// Open creates dir if needed, migrates the schema, and starts the writer.
func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "events.db")
	// auto_vacuum must come first: it only takes effect on a brand-new file.
	dsn := "file:" + path + "?_pragma=auto_vacuum(2)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=cache_size(-2000)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(1) // ponytail: one connection; add a read-only pool for the query API in M2
	if err := migrate(sdb, path); err != nil {
		sdb.Close()
		return nil, err
	}
	d := &DB{sql: sdb, path: path, in: make(chan []Event, queueSize), done: make(chan struct{})}
	d.day = time.Now().UnixMilli() / msPerDay
	sdb.QueryRow("SELECT count(*) FROM events WHERE ts >= ?", d.day*msPerDay).Scan(&d.today)
	d.measure()
	go d.run()
	return d, nil
}

// Accept applies the tenant's caps, then queues events for the writer. It never blocks.
func (d *DB) Accept(evs []Event, dailyCap, maxDBMB int) error {
	if d.bytes.Load() > int64(maxDBMB)<<20 {
		return ErrFull
	}
	d.mu.Lock()
	defer d.mu.Unlock() // held across the non-blocking send, so Close can never close the channel under us
	if d.closed {
		return ErrClosed
	}
	if day := time.Now().UnixMilli() / msPerDay; day != d.day {
		d.day, d.today = day, 0
	}
	if d.today+len(evs) > dailyCap {
		return ErrCap
	}
	select {
	case d.in <- evs:
		d.today += len(evs)
		return nil
	default:
		return ErrBusy
	}
}

// run is the single writer: it batches events and commits every flushEvery or flushAt events.
func (d *DB) run() {
	defer close(d.done)
	var buf []Event
	tick := time.NewTicker(flushEvery)
	defer tick.Stop()
	flush := func() {
		if len(buf) > 0 {
			d.insert(buf)
			buf = buf[:0]
		}
	}
	for {
		select {
		case evs, ok := <-d.in:
			if !ok {
				flush()
				return
			}
			buf = append(buf, evs...)
			if len(buf) >= flushAt {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func (d *DB) insert(evs []Event) {
	tx, err := d.sql.Begin()
	if err != nil {
		slog.Error("store: begin", "err", err, "lost_events", len(evs))
		return
	}
	stmt, err := tx.Prepare("INSERT INTO events (ts, name, props) VALUES (?, ?, ?)")
	if err == nil {
		for _, e := range evs {
			if _, err = stmt.Exec(e.TS, e.Name, e.Props); err != nil {
				break
			}
		}
		stmt.Close()
	}
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		slog.Error("store: insert", "err", err, "lost_events", len(evs))
		return
	}
	d.measure()
}

func (d *DB) measure() {
	var n int64
	for _, suffix := range []string{"", "-wal"} {
		if fi, err := os.Stat(d.path + suffix); err == nil {
			n += fi.Size()
		}
	}
	d.bytes.Store(n)
}

// Prune deletes events older than days (0 = keep forever) in small chunks so the writer is never starved.
func (d *DB) Prune(days int) error {
	if days <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	for {
		res, err := d.sql.Exec("DELETE FROM events WHERE id IN (SELECT id FROM events WHERE ts < ? LIMIT ?)", cutoff, pruneChunk)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, err := d.sql.Exec("PRAGMA incremental_vacuum(2000)")
	d.measure()
	return err
}

// Close drains the queue, checkpoints the WAL and closes the file. Call after the HTTP servers have stopped.
func (d *DB) Close() error {
	d.mu.Lock()
	d.closed = true
	close(d.in)
	d.mu.Unlock()
	<-d.done
	d.sql.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return d.sql.Close()
}

// Manager holds the open tenant databases.
type Manager struct {
	dir string
	mu  sync.RWMutex
	dbs map[string]*DB
}

func NewManager(dataDir string) *Manager { return &Manager{dir: dataDir, dbs: map[string]*DB{}} }

// Open opens a tenant's DB if it isn't open yet. A failure (e.g. newer schema) affects only that tenant.
func (m *Manager) Open(tenant string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dbs[tenant] != nil {
		return nil
	}
	d, err := Open(filepath.Join(m.dir, tenant))
	if err != nil {
		return fmt.Errorf("tenant %q: %w", tenant, err)
	}
	m.dbs[tenant] = d
	return nil
}

// Retain closes (and flushes) every open tenant not in keep. Used on config reload.
func (m *Manager) Retain(keep map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.dbs {
		if !keep[id] {
			if err := d.Close(); err != nil {
				slog.Error("close", "tenant", id, "err", err)
			}
			delete(m.dbs, id)
		}
	}
}

// Get returns nil if the tenant's DB failed to open.
func (m *Manager) Get(tenant string) *DB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dbs[tenant]
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.dbs {
		if err := d.Close(); err != nil {
			slog.Error("close", "tenant", id, "err", err)
		}
	}
	m.dbs = map[string]*DB{}
}
