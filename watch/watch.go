package watch

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/mindsgn-studio/takealot-scraper/category"
	"github.com/mindsgn-studio/takealot-scraper/database"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var db *sql.DB
var client = &http.Client{}

var (
	itemNumber = 0
	skip       = 0
)

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

func getList(category string) {
	limit := 100
	db := database.ConnectDatabase()

	query := `
    WITH items_cte AS (
        SELECT api, item_id
        FROM items
        WHERE source LIKE $1 AND category LIKE $2
        LIMIT $3
        OFFSET $4
    ), count_cte AS (
        SELECT COUNT(*) AS total_count
        FROM items
        WHERE source LIKE $1 AND category LIKE $2
    )
    SELECT api, item_id, total_count
    FROM items_cte, count_cte;`

	rows, err := db.Query(query, "takealot", category, limit, skip*limit)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	defer db.Close()

	var totalCount int

	for rows.Next() {
		var (
			api    string
			itemID string
		)

		if err := rows.Scan(&api, &itemID, &totalCount); err != nil {
			log.Fatal(err)
		}

		itemNumber++
		if api != "" && itemNumber < totalCount {
			fmt.Printf("Processing item %d/%d\n", itemNumber, totalCount)
			getTakealotProduct(api, itemID)
		} else if itemNumber == totalCount {
			fmt.Printf("Reached total count of %d items. Stopping further processing.\n", totalCount)
			itemNumber = 0
			skip = 0
			Watch()
			break
		}
	}

	if err := rows.Err(); err != nil {
		fmt.Println(err)
		return
	}

	skip++
	getList(category)
}

func Watch() {
	db = database.ConnectDatabase()
	category := category.GetRandomCategory()
	fmt.Println("======================================================================")
	fmt.Println("Category:", category)
	fmt.Println("======================================================================")
	getList(category)
}
