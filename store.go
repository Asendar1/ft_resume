package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Status string

const (
	StatusApplied   Status = "applied"
	StatusScreen    Status = "screen"
	StatusInterview Status = "interview"
	StatusOffer     Status = "offer"
	StatusRejected  Status = "rejected"
)

func (s Status) Valid() bool {
	switch s {
	case StatusApplied, StatusScreen, StatusInterview, StatusOffer, StatusRejected:
		return true
	}
	return false
}

var (
	ErrNotFound  = errors.New("application not found")
	ErrDuplicate = errors.New("application already recorded for this company and role")
)

type Application struct {
	ID        int64  `json:"id"`
	Company   string `json:"company"`
	Role      string `json:"role"`
	Status    Status `json:"status"`
	Notes     string `json:"notes"`
	CreatedAt string `json:"created_at"`
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, a Application) (Application, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO applications (company, role, status, notes) VALUES (?, ?, ?, ?)`,
		a.Company, a.Role, a.Status, a.Notes)
	if err != nil {
		// ponytail: string match on the driver error; swap for a typed sqlite error if a
		// second constraint class ever needs distinguishing.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Application{}, ErrDuplicate
		}
		return Application{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Application{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id int64) (Application, error) {
	var a Application
	err := s.db.QueryRowContext(ctx,
		`SELECT id, company, role, status, notes, created_at FROM applications WHERE id = ?`, id).
		Scan(&a.ID, &a.Company, &a.Role, &a.Status, &a.Notes, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	return a, err
}

func (s *Store) List(ctx context.Context, status Status, limit int) ([]Application, error) {
	q := `SELECT id, company, role, status, notes, created_at FROM applications`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Application, 0, limit)
	for rows.Next() {
		var a Application
		if err := rows.Scan(&a.ID, &a.Company, &a.Role, &a.Status, &a.Notes, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateStatus(ctx context.Context, id int64, status Status) (Application, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE applications SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return Application{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Application{}, err
	}
	if n == 0 {
		return Application{}, ErrNotFound
	}
	return s.Get(ctx, id)
}
