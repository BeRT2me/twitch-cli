// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package database

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
)

// dbtx is the part of *sqlx.DB and *sqlx.Tx that queries use, so a Query can run inside a caller's transaction.
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	NamedExec(query string, arg any) (sql.Result, error)
	NamedQuery(query string, arg any) (*sqlx.Rows, error)
	Select(dest any, query string, args ...any) error
	Get(dest any, query string, args ...any) error
}

// queryTx is what a query needs from the transaction returned by begin.
type queryTx interface {
	Exec(query string, args ...any) (sql.Result, error)
	NamedExec(query string, arg any) (sql.Result, error)
	Commit() error
	Rollback() error
}

// finish ends a transaction from begin: a failed statement rolls it back and is returned, otherwise it commits.
func finish(tx queryTx, err error) error {
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

type Query struct {
	Limit  int
	Cursor string
	InternalPagination
	DB dbtx
}

// savepoint nests a query's transaction inside a caller's, since SQLite rejects a second BEGIN.
type savepoint struct{ *sqlx.Tx }

func (s savepoint) Commit() error {
	_, err := s.Exec("RELEASE query")
	return err
}

func (s savepoint) Rollback() error {
	_, err := s.Exec("ROLLBACK TO query; RELEASE query")
	return err
}

func (q *Query) begin() queryTx {
	if tx, ok := q.DB.(*sqlx.Tx); ok {
		tx.MustExec("SAVEPOINT query")
		return savepoint{tx}
	}
	return q.DB.(*sqlx.DB).MustBegin()
}

// NewQuery handles the logic for generating the pagination token to pass alongside the DB queries for easier access
func (c CLIDatabase) NewQuery(r *http.Request, max_limit int) *Query {
	return c.NewQueryWithDefaultLimit(r, max_limit, 20)
}

func (c CLIDatabase) NewQueryWithDefaultLimit(r *http.Request, max_limit int, default_limit int) *Query {
	p := Query{DB: c.DB}
	if c.Tx != nil {
		p.DB = c.Tx
	}
	if r == nil {
		return &p
	}

	ic := InternalCursor{}

	query := r.URL.Query()
	a := query.Get("after")
	f := query.Get("first")
	b := query.Get("before")

	isBefore := false
	if b != "" {
		isBefore = true
	}

	if len(a) > 0 {
		p.Cursor = a
	}

	first, _ := strconv.Atoi(f)
	if first > max_limit || first <= 0 {
		first = default_limit
	}
	p.Limit = int(first)

	if a != "" {
		b, err := base64.RawStdEncoding.DecodeString(a)
		if err != nil {
			return &p
		}
		err = json.Unmarshal(b, &ic)
		if err != nil {
			return &p
		}

		if isBefore {
			ic.Offset -= first
		} else {
			ic.Offset += first
		}
	}

	ic.Limit = first

	if ic.Offset < 0 {
		return &p
	}

	body, _ := json.Marshal(ic)

	ip := InternalPagination{
		InternalCursor:   ic,
		PaginationCursor: base64.RawURLEncoding.EncodeToString(body),
		SQL:              fmt.Sprintf(" LIMIT %v OFFSET %v", ic.Limit, ic.Offset),
	}

	p.InternalPagination = ip
	return &p
}
