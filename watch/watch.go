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
var skip uint64 = 0

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

func saveItemPrice(price float64, title string, brand string, link string) {
	return
}

func getTakealotProduct(api string, title string, link string) {
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

	saveItemPrice(price, title, data.Core.Brand, link)
}

func getList() {
	db = database.ConnectDatabase()
	defer db.Close()

	query := `
		SELECT api, title, link
		FROM items
		LIMIT 100
		OFFSET $1;`

	rows, err := db.Query(query, skip)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			api   string
			title string
			link  string
		)

		if err := rows.Scan(&api, &title, &link); err != nil {
			log.Fatal(err)
		}

		if api != "" {
			fmt.Println(api, title, link)
		}
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
}

func Watch() {
	getList()
}
