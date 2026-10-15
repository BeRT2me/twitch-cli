// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package database

import (
	"log"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/spf13/viper"
	"github.com/twitchdev/twitch-cli/internal/util"

	// sqlite3 driver
	_ "github.com/mattn/go-sqlite3"
)

var dbFileName = "eventCache.db"

type CLIDatabase struct {
	DB *sqlx.DB
	// Tx, when set, makes every query from NewQuery run inside it.
	Tx *sqlx.Tx
}

func NewConnection(extendedBusyTimeout bool) (CLIDatabase, error) {
	db, err := getDatabase(extendedBusyTimeout)
	if err != nil {
		return CLIDatabase{}, err
	}

	return CLIDatabase{DB: &db}, nil
}

// extendedBusyTimeout sets an extended timeout for waiting on a busy database. This is mainly an issue in tests on WSL, so this flag shouldn't be used in production.
func getDatabase(extendedBusyTimeout bool) (sqlx.DB, error) {
	home, err := util.GetApplicationDir()
	if err != nil {
		return sqlx.DB{}, err
	}

	if viper.GetString("DB_FILENAME") != "" {
		dbFileName = viper.GetString("DB_FILENAME")
	}

	var path = filepath.Join(home, dbFileName)

	// force Foreign Key support ("fk=true")
	dbFlags := "?_fk=true&cache=shared"
	if extendedBusyTimeout {
		// https://www.sqlite.org/c3ref/busy_timeout.html
		dbFlags += "&_busy_timeout=60000"
	}
	for i := 0; i <= 5; i++ {
		db, err := sqlx.Open("sqlite3", path+dbFlags)
		if err != nil {
			log.Print(i)
			if i == 5 {
				return sqlx.DB{}, err
			}
			continue
		}

		// Decide from the schema rather than from whether the file exists: a first run killed during
		// initialization leaves an empty file, which would otherwise skip init and then fail every migration.
		var tables int
		err = db.Get(&tables, "select count(*) from sqlite_master")
		if err != nil {
			db.Close()
			return sqlx.DB{}, err
		}
		if tables == 0 {
			err = initDatabase(*db)
			if err != nil {
				log.Printf("%#v", err)
				db.Close()
				return sqlx.DB{}, err
			}
		}
		db.SetMaxOpenConns(1)
		err = checkAndUpdate(*db)
		if err != nil {
			os.Exit(99)
		}

		return *db, nil
	}
	return sqlx.DB{}, nil
}
