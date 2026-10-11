package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Migration is one versioned schema change. Versions start at 1 and grow by
// one; an applied migration is never edited, a new one is appended.
type Migration struct {
	Version int
	SQL     string
}

// Migrations is cogtest's schema history.
var Migrations = []Migration{
	{Version: 1, SQL: `
CREATE TABLE quizzes (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	date           TEXT    NOT NULL,
	status         TEXT    NOT NULL CHECK (status IN ('generating', 'ready', 'failed', 'expired')),
	trigger        TEXT    NOT NULL DEFAULT '',
	model          TEXT    NOT NULL DEFAULT '',
	effort         TEXT    NOT NULL DEFAULT '',
	run_id         TEXT    NOT NULL DEFAULT '',
	question_count INTEGER NOT NULL DEFAULT 0,
	focus          TEXT    NOT NULL DEFAULT '[]',
	error          TEXT    NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL,
	updated_at     INTEGER NOT NULL
);
CREATE INDEX quizzes_date ON quizzes (date);
CREATE INDEX quizzes_run_id ON quizzes (run_id);

CREATE TABLE questions (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	quiz_id       INTEGER NOT NULL REFERENCES quizzes (id) ON DELETE CASCADE,
	idx           INTEGER NOT NULL,
	question      TEXT    NOT NULL,
	choices       TEXT    NOT NULL,
	correct_index INTEGER NOT NULL,
	explanation   TEXT    NOT NULL,
	anchors       TEXT    NOT NULL,
	fingerprint   TEXT    NOT NULL,
	area          TEXT    NOT NULL,
	UNIQUE (quiz_id, idx)
);
CREATE INDEX questions_fingerprint ON questions (fingerprint);

CREATE TABLE attempts (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	question_id  INTEGER NOT NULL UNIQUE REFERENCES questions (id) ON DELETE CASCADE,
	chosen_index INTEGER NOT NULL,
	correct      INTEGER NOT NULL,
	answered_at  INTEGER NOT NULL
);

CREATE TABLE mastery (
	area       TEXT    PRIMARY KEY,
	box        INTEGER NOT NULL,
	due        TEXT    NOT NULL,
	correct    INTEGER NOT NULL DEFAULT 0,
	wrong      INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL
);

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`},
	// 2: re-asks of missed questions and anchors whose code changed.
	{Version: 2, SQL: `
ALTER TABLE quizzes ADD COLUMN reasks TEXT NOT NULL DEFAULT '[]';
ALTER TABLE questions ADD COLUMN reask_of INTEGER NOT NULL DEFAULT 0;
ALTER TABLE questions ADD COLUMN stale INTEGER NOT NULL DEFAULT 0;
CREATE INDEX questions_reask_of ON questions (reask_of);
`},
}

// migrate applies the migrations db hasn't seen yet, each in its own
// transaction together with its version row.
func migrate(ctx context.Context, db *sql.DB, migrations []Migration) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("reading the schema version: %w", err)
	}
	for i, m := range migrations {
		if m.Version != i+1 {
			return fmt.Errorf("migration %d has version %d", i+1, m.Version)
		}
		if m.Version <= current {
			continue
		}
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("migration %d: %w", m.Version, err)
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, m.Version); err != nil {
		return err
	}
	return tx.Commit()
}
