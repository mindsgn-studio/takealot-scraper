package watch

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/mindsgn-studio/takealot-scraper/database"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var db *sql.DB
var client = &http.Client{}
var skip int = 0

type Sources struct {
	API string `bson:"api"`
}

type Item struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Title   string             `bson:"title"`
	Brand   string             `bson:"brand"`
	Link    string             `bson:"link"`
	Images  []string           `bson:"images"`
	Price   float64            `bson:"price"`
	Sources Sources            `bson:"sources"`
}

type BuyBox struct {
	Prices []float64 `bson:"prices"`
}

type Core struct {
	Brand string `bson:"brand"`
}

type TakealotResponse struct {
	BuyBox BuyBox `bson:"buybox"`
	Core   Core   `bson:"core"`
}

type Price struct {
	ItemID   string    `bson:"ItemID"`
	Date     time.Time `bson:"date"`
	Currency string    `bson:"currency"`
	Price    float64   `bson:"price"`
}

func saveItemPrice(price float64, itemID string) {
	db := database.ConnectDatabase()

	sqlStatement := `
	INSERT INTO price (item_id, currency, price)
	VALUES ($1, $2, $3)`

	defer db.Close()

	_, err := db.Exec(sqlStatement, itemID, "zar", price)
	if err != nil {
		log.Fatalf("Error inserting into items table: %v", err)
	}

	fmt.Println(itemID, "saved!")
	return
}

func getTakealotProduct(api string, itemID string) {
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var data TakealotResponse
	err = json.Unmarshal(body, &data)
	if err != nil {
		return
	}

	price := data.BuyBox.Prices[0]

	saveItemPrice(price, itemID)
}

func getList() {
	limit := 100
	db := database.ConnectDatabase()

	query := `
		SELECT api, item_id
		FROM items
		WHERE source LIKE $1
		LIMIT $2
		OFFSET $3;`

	rows, err := db.Query(query, "takealot", limit, skip*limit)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	defer db.Close()

	for rows.Next() {
		var (
			api    string
			itemID string
		)

		if err := rows.Scan(&api, &itemID); err != nil {
			log.Fatal(err)
		}

		if api != "" {
			getTakealotProduct(api, itemID)
		}
	}

	if err := rows.Err(); err != nil {
		fmt.Println(err)
		return
	}
}

func Watch() {
	for {
		getList()
		fmt.Println(skip)
		skip++
	}
}
