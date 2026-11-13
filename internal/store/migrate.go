package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SchemaVersion is the newest schema this binary knows (= number of migration files).
func SchemaVersion() int {
	files, _ := fs.Glob(migrationFS, "migrations/*.sql")
	return len(files)
}

// migrate brings the DB to SchemaVersion(). Migrations are numbered, forward-only, embedded,
// and each runs in a transaction together with the user_version bump.
// Existing data is copied to <db>.pre-v<N>.bak first; the copy is deleted on the next start that needs no migration.
func migrate(db *sql.DB, path string) error {
	files, err := fs.Glob(migrationFS, "migrations/*.sql") // Glob returns lexical order: 0001, 0002, ...
	if err != nil {
		return err
	}
	var cur int
	if err := db.QueryRow("PRAGMA user_version").Scan(&cur); err != nil {
		return err
	}
	switch {
	case cur > len(files):
		return fmt.Errorf("database schema v%d is newer than this binary (v%d): upgrade tallymoth, or restore a backup", cur, len(files))
	case cur == len(files):
		stale, _ := filepath.Glob(path + ".pre-v*.bak")
		for _, f := range stale {
			os.Remove(f)
		}
		return nil
	}
	if cur > 0 {
		bak := fmt.Sprintf("%s.pre-v%d.bak", path, cur)
		os.Remove(bak) // VACUUM INTO refuses to overwrite
		if _, err := db.Exec("VACUUM INTO ?", bak); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	for i := cur; i < len(files); i++ {
		sqlText, _ := migrationFS.ReadFile(files[i])
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		_, err = tx.Exec(string(sqlText))
		if err == nil {
			_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)) // rolls back with the tx
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", files[i], err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
