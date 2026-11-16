package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
)

const indexSchema = `
CREATE TABLE IF NOT EXISTS helper_ids (
	address TEXT PRIMARY KEY,
	id      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS helper_conversations (
	id            TEXT PRIMARY KEY,
	kind          TEXT NOT NULL,
	name          TEXT NOT NULL DEFAULT '',
	last_activity INTEGER NOT NULL DEFAULT 0,
	last_read     INTEGER NOT NULL DEFAULT 0,
	unread        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS helper_conversations_by_activity ON helper_conversations (last_activity DESC, id);
`

type index struct {
	db  *sql.DB
	log zerolog.Logger
}

type chatNote struct {
	id     string
	kind   string
	name   string
	at     time.Time
	unread bool
}

type chatRow struct {
	id           string
	kind         string
	name         string
	lastActivity time.Time
	lastRead     time.Time
	unread       bool
}

type chatPage struct {
	rows []chatRow
	next string
}

func openIndex(ctx context.Context, db *sql.DB, log zerolog.Logger) (*index, error) {
	if _, err := db.ExecContext(ctx, indexSchema); err != nil {
		return nil, fmt.Errorf("prepare conversation index: %w", err)
	}
	return &index{db: db, log: log}, nil
}

func (x *index) pin(ctx context.Context, addresses ...types.JID) (string, error) {
	var known []string
	for _, address := range addresses {
		if !address.IsEmpty() {
			known = append(known, address.ToNonAD().String())
		}
	}
	if len(known) == 0 {
		return "", errors.New("no address to pin")
	}
	id := known[0]
	for _, address := range known {
		var pinned string
		err := x.db.QueryRowContext(ctx, `SELECT id FROM helper_ids WHERE address = ?`, address).Scan(&pinned)
		if err == nil {
			id = pinned
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	for _, address := range known {
		if _, err := x.db.ExecContext(ctx, `INSERT OR IGNORE INTO helper_ids (address, id) VALUES (?, ?)`, address, id); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (x *index) addresses(ctx context.Context, id string) ([]types.JID, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT address FROM helper_ids WHERE id = ? ORDER BY address = id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.JID
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, err
		}
		if jid, err := types.ParseJID(address); err == nil {
			out = append(out, jid)
		}
	}
	return out, rows.Err()
}

func (x *index) get(ctx context.Context, id string) (chatRow, bool) {
	row := chatRow{id: id}
	var activity, read int64
	err := x.db.QueryRowContext(ctx, `SELECT kind, name, last_activity, last_read, unread FROM helper_conversations WHERE id = ?`, id).
		Scan(&row.kind, &row.name, &activity, &read, &row.unread)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			x.log.Error().Err(err).Msg("Could not read the conversation index")
		}
		return chatRow{}, false
	}
	row.lastActivity, row.lastRead = time.UnixMilli(activity).UTC(), time.UnixMilli(read).UTC()
	return row, true
}

func (x *index) put(ctx context.Context, row chatRow) bool {
	_, err := x.db.ExecContext(ctx, `INSERT INTO helper_conversations (id, kind, name, last_activity, last_read, unread) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, name = excluded.name, last_activity = excluded.last_activity, last_read = excluded.last_read, unread = excluded.unread`,
		row.id, row.kind, row.name, row.lastActivity.UnixMilli(), row.lastRead.UnixMilli(), row.unread)
	if err != nil {
		x.log.Error().Err(err).Msg("Could not write the conversation index")
		return false
	}
	return true
}

func (x *index) note(ctx context.Context, n chatNote) (chatRow, bool) {
	row, exists := x.get(ctx, n.id)
	before := row
	if !exists {
		row = chatRow{id: n.id, kind: n.kind, lastActivity: time.UnixMilli(0).UTC(), lastRead: time.UnixMilli(0).UTC()}
	}
	if n.name != "" {
		row.name = n.name
	}
	if n.at.After(row.lastActivity) {
		row.lastActivity = n.at.Truncate(time.Millisecond).UTC()
	}
	if n.unread && n.at.After(row.lastRead) {
		row.unread = true
	}
	if exists && row == before {
		return row, false
	}
	return row, x.put(ctx, row)
}

func (x *index) markRead(ctx context.Context, id string, at time.Time) (chatRow, bool) {
	row, exists := x.get(ctx, id)
	if !exists {
		return row, false
	}
	before := row
	if at.After(row.lastRead) {
		row.lastRead = at.Truncate(time.Millisecond).UTC()
	}
	if row.unread && !row.lastRead.Before(row.lastActivity) {
		row.unread = false
	}
	if row == before {
		return row, false
	}
	return row, x.put(ctx, row) && row.unread != before.unread
}

func (x *index) page(ctx context.Context, before string, limit int) (chatPage, error) {
	query := `SELECT id FROM helper_conversations ORDER BY last_activity DESC, id LIMIT ?`
	args := []any{limit + 1}
	if before != "" {
		activity, id, ok := strings.Cut(before, "|")
		ms, err := strconv.ParseInt(activity, 10, 64)
		if !ok || err != nil {
			return chatPage{}, errBadCursor
		}
		query = `SELECT id FROM helper_conversations WHERE last_activity < ? OR (last_activity = ? AND id > ?) ORDER BY last_activity DESC, id LIMIT ?`
		args = []any{ms, ms, id, limit + 1}
	}
	rows, err := x.db.QueryContext(ctx, query, args...)
	if err != nil {
		return chatPage{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return chatPage{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return chatPage{}, err
	}
	var page chatPage
	for i, id := range ids {
		if i == limit {
			last := page.rows[len(page.rows)-1]
			page.next = strconv.FormatInt(last.lastActivity.UnixMilli(), 10) + "|" + last.id
			break
		}
		if row, ok := x.get(ctx, id); ok {
			page.rows = append(page.rows, row)
		}
	}
	return page, nil
}
