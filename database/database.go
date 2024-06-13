package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

const (
	host     = "api.snapprice.co.za"
	port     = 5432
	user     = "seni"
	password = "Qe6kpnhnn7Xd3367MguD"
	dbname   = "snapprice"
)

func ConnectDatabase() *sql.DB {
	dbinfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)
	db, err := sql.Open("postgres", dbinfo)
	if err != nil {
		log.Fatal(err)
	}

	// defer db.Close()

	return db
}
