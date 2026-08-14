// Package sqlite is the single SQLite driver registration point for Mneme.
//
// It imports the ncruces/go-sqlite3 database/sql driver (registered under the
// name "sqlite3") and installs the FTS5 and vec1 extensions on every new
// connection via sqlite3.AutoExtension. Importing this package is equivalent
// to the old `_ "github.com/mattn/go-sqlite3"` blank import, plus the
// extensions that the memory subsystem relies on (FTS5 full-text search and
// vec1 vector search).
//
// Because both drivers register under "sqlite3", mattn/go-sqlite3 must NOT be
// imported anywhere alongside this package.
package sqlite

import (
	"database/sql"

	sqlite3 "github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	"github.com/ncruces/go-sqlite3/ext/vec1"
)

func init() {
	// AutoExtension applies these registration callbacks to every connection
	// the driver opens, so FTS5 and vec1 virtual tables are always available.
	sqlite3.AutoExtension(fts5.Register)
	sqlite3.AutoExtension(vec1.Register)
}

// Open opens a SQLite database by path, enabling WAL and a busy timeout. It is
// a thin convenience wrapper so callers do not need to remember the pragmas.
// The ncruces driver treats a plain path as a filename, so PRAGMAs must be
// issued explicitly (mattn-style `?_journal=WAL` query strings are not parsed).
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
