package main

import (
	"database/sql"
	"log"

	_ "github.com/ncruces/go-sqlite3/driver"
)

type File struct {
	ID           string
	OriginalName string
	Path         string
	Expiry       int64
}

var db *sql.DB

func InitDB(filepath string) {
	conn, err := sql.Open("sqlite3", filepath)
	if err != nil {
		log.Fatal("failed to open database:", err)
	}

	var journalMode string
	if err := conn.QueryRow("PRAGMA journal_mode=WAL;").Scan(&journalMode); err != nil {
		log.Fatal("failed to set WAL mode:", err)
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(4)
	_, _ = conn.Exec("PRAGMA synchronous=NORMAL;")
	_, _ = conn.Exec("PRAGMA foreign_keys = ON;")

	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id            TEXT PRIMARY KEY,
		original_name TEXT NOT NULL,
		path          TEXT NOT NULL,
		expiry        INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS links (
		id            TEXT PRIMARY KEY,
		file_id       TEXT NOT NULL,
		FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
	);`
	if _, err := conn.Exec(schema); err != nil {
		log.Fatal("schema thing failed:", err)
	}

	db = conn

	migration := `
	INSERT INTO links (id, file_id)
	SELECT id, id FROM files WHERE id NOT IN (SELECT DISTINCT file_id FROM links);`
	if _, err := db.Exec(migration); err != nil {
		log.Println("migration warning:", err)
	}
}

func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func SaveFileWithLinks(id, originalName, path string, expiry int64, linkIDs []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO files (id, original_name, path, expiry) VALUES (?, ?, ?, ?);", id, originalName, path, expiry)
	if err != nil {
		return err
	}

	for _, linkID := range linkIDs {
		_, err = tx.Exec("INSERT INTO links (id, file_id) VALUES (?, ?);", linkID, id)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func GetFileByLink(linkID string) (*File, error) {
	query := `
		SELECT f.id, f.original_name, f.path, f.expiry
		FROM files f
		JOIN links l ON f.id = l.file_id
		WHERE l.id = ?;`
	f := new(File)
	err := db.QueryRow(query, linkID).Scan(&f.ID, &f.OriginalName, &f.Path, &f.Expiry)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func DeleteFile(id string) error {
	query := `DELETE FROM files WHERE id = ?;`
	_, err := db.Exec(query, id)
	return err
}

func ClaimLink(linkID string) (*File, bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	query := `
		SELECT f.id, f.original_name, f.path, f.expiry
		FROM files f
		JOIN links l ON f.id = l.file_id
		WHERE l.id = ?;`
	f := new(File)
	err = tx.QueryRow(query, linkID).Scan(&f.ID, &f.OriginalName, &f.Path, &f.Expiry)
	if err != nil {
		return nil, false, err
	}

	_, err = tx.Exec("DELETE FROM links WHERE id = ?;", linkID)
	if err != nil {
		return nil, false, err
	}

	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM links WHERE file_id = ?;", f.ID).Scan(&count)
	if err != nil {
		return nil, false, err
	}

	lastLink := count == 0
	if lastLink {
		_, err = tx.Exec("DELETE FROM files WHERE id = ?;", f.ID)
		if err != nil {
			return nil, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	return f, lastLink, nil
}