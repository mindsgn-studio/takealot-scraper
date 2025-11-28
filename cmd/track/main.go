package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"

	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/internal/core"
)

type Watch struct {
	Item_ID sql.NullString `json:"item_id"`
	Token   sql.NullString `json:"token"`
	Device  sql.NullString `json:"device"`
}

type Item struct {
	UUID        string `json:"uuid"`
	Link        string `json:"link"`
	Source_Name string `json:"source_name"`
}

func main() {
	logger := log.New(os.Stdout, "[track] ", log.LstdFlags|log.Lmsgprefix)
	logger.Println("Starting to track items...")

	pgDB, err := core.ConnectPostgres()
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}
	defer pgDB.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		getList(pgDB)
	}()

	wg.Wait()

	logger.Println("Migration completed successfully!")
}

func assessItem(pgDB *sql.DB, uuid string) {
	query := `SELECT link, uuid, source_name FROM items WHERE uuid = $1`

	rows, err := pgDB.Query(query, uuid)
	if err != nil {
		log.Print(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Link, &item.UUID, &item.Source_Name); err != nil {
			log.Println("Error scanning price:", err)
		}

		if item.Source_Name == "takealot" {
			item, err := core.OpenPageTakealot(item.Link)
			if err != nil {
				log.Println("Error scanning price:", err)
				return
			}

			if item.Current_Price == 0 {
				return
			}

			core.SavePrice(item.Current_Price, uuid)
		}

		if item.Source_Name == "shoprite" {
			item, err := core.OpenPageTakealot(item.Link)
			if err != nil {
				log.Println("Error scanning price:", err)
				return
			}

			if item.Current_Price == 0 {
				return
			}

			core.SavePrice(item.Current_Price, uuid)
		}
	}
}

func getList(pgDB *sql.DB) {
	query := `
			SELECT item_id, token, device FROM watch
		`

	rows, err := pgDB.Query(query)
	if err != nil {
		fmt.Print(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		var watch Watch
		if err := rows.Scan(&watch.Item_ID, &watch.Token, &watch.Device); err != nil {
			log.Println("Error scanning item:", err)
			continue
		}

		assessItem(pgDB, watch.Item_ID.String)

		if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "ios" {
			core.IOSPushNotification(watch.Token.String)
		}

		if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "android" {
			core.AndroidpushhNotification(watch.Token.String)
		}
	}
}
