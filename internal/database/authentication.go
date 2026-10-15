// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package database

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/twitchdev/twitch-cli/internal/util"
)

type AuthenticationClient struct {
	ID          string `db:"id"`
	Secret      string `db:"secret"`
	Name        string `db:"name"`
	IsExtension bool   `db:"is_extension"`
}

type Authorization struct {
	ID        int    `db:"id" dbi:"false"`
	ClientID  string `db:"client_id"`
	UserID    string `db:"user_id"`
	Token     string `db:"token"`
	ExpiresAt string `db:"expires_at"`
	Scopes    string `db:"scopes"`
}

func (q *Query) GetAuthorizationByToken(token string) (Authorization, error) {
	var r Authorization
	db := q.DB

	err := db.Get(&r, "select * from authorizations where token = $1", token)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	} else if err != nil {
		return r, err
	}

	return r, err
}

func (q *Query) InsertOrUpdateAuthenticationClient(client AuthenticationClient, upsert bool) (AuthenticationClient, error) {
	db := q.DB

	stmt := `insert into clients values(:id, :secret, :is_extension, :name)`
	if upsert {
		stmt += ` on conflict(id) do update set secret=:secret, is_extension:is_extension, name=:name`
	}

	client.Secret = generateString(30)

	// Only an ID collision is retried, with a fresh random ID; any other error is a real failure.
	for i := 0; ; i++ {
		_, err := db.NamedExec(generateInsertSQL("clients", "id", client, upsert), client)
		if err == nil || !isUniqueViolation(err) || i == maxIDRetries {
			return client, err
		}
		client.ID = util.RandomClientID()
	}
}

func (q *Query) CreateAuthorization(a Authorization) (Authorization, error) {
	a.Token = generateString(15)
	a.ExpiresAt = util.GetTimestamp().Add(24 * 30 * time.Hour).Format(time.RFC3339Nano)

	// loop to create unique tokens; likely won't happen, but is worth handling regardless
	for i := 0; ; i++ {
		tx := q.begin()
		stmt := generateInsertSQL("authorizations", "", a, false)
		_, err := tx.NamedExec(stmt, a)
		if err == nil {
			err = tx.Commit()
		}
		if err == nil {
			return a, nil
		}
		tx.Rollback()
		if !isUniqueViolation(err) || i == maxIDRetries {
			return a, err
		}
		a.Token = generateString(15)
	}
}

// maxIDRetries bounds how often a random ID or token is regenerated after a collision.
const maxIDRetries = 10

// isUniqueViolation reports whether err is a collision on a unique column or on a primary key, which
// SQLite reports with different codes but the same "UNIQUE constraint failed" message.
func isUniqueViolation(err error) bool {
	var se sqlite3.Error
	if !errors.As(err, &se) {
		return false
	}
	return se.ExtendedCode == sqlite3.ErrConstraintUnique || se.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
}

func (q *Query) GetAuthenticationClient(ac AuthenticationClient) (*DBResponse, error) {
	var r []AuthenticationClient
	rows, err := q.DB.NamedQuery(generateSQL("select * from clients", ac, SEP_AND)+q.SQL, ac)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var ac AuthenticationClient
		err := rows.StructScan(&ac)
		if err != nil {
			return nil, err
		}
		r = append(r, ac)
	}

	dbr := DBResponse{
		Data:  r,
		Limit: q.Limit,
		Total: len(r),
	}

	if len(r) != q.Limit {
		q.PaginationCursor = ""
	}

	dbr.Cursor = q.PaginationCursor

	return &dbr, err
}

func generateString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:length]
}
