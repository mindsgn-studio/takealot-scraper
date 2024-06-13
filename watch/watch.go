package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoClient *mongo.Client

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
	db := mongoClient.Database("snapprice")
	itemCollection := db.Collection("items")
	pricesCollection := db.Collection("prices")
	twelveHoursAgo := time.Now().Add(-12 * time.Hour)

	filter := map[string]interface{}{
		"link": link,
	}

	var result map[string]interface{}

	err := itemCollection.FindOne(context.Background(), filter).Decode(&result)
	if err != nil {
		return
	}

	if id, ok := result["_id"].(primitive.ObjectID); ok {
		itemID := id.Hex()
		filter := map[string]interface{}{
			"itemID": itemID,
			"date":   map[string]interface{}{"$gt": twelveHoursAgo},
		}

		var result map[string]interface{}
		err := pricesCollection.FindOne(context.Background(), filter).Decode(&result)
		if err != nil {
			newPrice := &Price{
				ItemID:   itemID,
				Date:     time.Now(),
				Currency: "zar",
				Price:    price,
			}

			_, err := pricesCollection.InsertOne(context.Background(), newPrice)
			if err != nil {
				fmt.Println(err)
				return
			}
		}
	}
}

func getTakealotProduct(api string, title string, link string) {
	client := &http.Client{}
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

func getList(skip int64) {
	opts := options.Find()
	opts.SetLimit(int64(100))
	opts.SetSkip(int64(skip))

	cursor, err := mongoClient.Database("snapprice").Collection("items").Find(context.Background(), bson.M{"sources.source": "takealot"}, opts)
	if err != nil {
		log.Println("Error finding documents:", err)
	}

	var items []Item
	if err := cursor.All(context.Background(), &items); err != nil {
		log.Println("Error decoding cursor:", err)
	}

	for _, item := range items {
		item.ID.Hex()
		getTakealotProduct(item.Sources.API, item.Title, item.Link)
	}

	skip++
	getList(skip)
}

func Watch() {
	getList(0)
}
