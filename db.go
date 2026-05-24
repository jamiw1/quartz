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

	schema := `
	CREATE TABLE IF NOT EXISTS files (
		id            TEXT PRIMARY KEY,
		original_name TEXT NOT NULL,
		path          TEXT NOT NULL,
		expiry        INTEGER NOT NULL
	);`
	if _, err := conn.Exec(schema); err != nil {
		log.Fatal("schema thing failed:", err)
	}

	db = conn
}

func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func SaveFile(id, originalName, path string, expiry int64) error {
	query := `INSERT INTO files (id, original_name, path, expiry) VALUES (?, ?, ?, ?);`
	_, err := db.Exec(query, id, originalName, path, expiry)
	return err
}

func GetFile(id string) (*File, error) {
	query := `SELECT id, original_name, path, expiry FROM files WHERE id = ?;`
	
	f := new(File)
	err := db.QueryRow(query, id).Scan(&f.ID, &f.OriginalName, &f.Path, &f.Expiry)
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

func ClaimFile(id string) (*File, error) {
	query := `DELETE FROM files WHERE id = ? RETURNING id, original_name, path, expiry;`
	f := new(File)
	err := db.QueryRow(query, id).Scan(&f.ID, &f.OriginalName, &f.Path, &f.Expiry)
	if err != nil {
		return nil, err
	}
	return f, nil
}