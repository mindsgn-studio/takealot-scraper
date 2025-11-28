package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/internal/core"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type MongoItem struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Title   string             `bson:"title"`
	Brand   string             `bson:"brand"`
	Link    string             `bson:"link"`
	Sources struct {
		ID     string `bson:"id"`
		Source string `bson:"source"`
	} `bson:"sources"`
	Images []string `bson:"images"`
}

type MongoPrice struct {
	ItemID string    `bson:"itemID"`
	Price  float64   `bson:"price"`
	Date   time.Time `bson:"date"`
}

type MongoWatch struct {
	ItemID  string    `bson:"itemID"`
	User    string    `bson:"user"`
	Created time.Time `bson:"created"`
}

type MongoSearch struct {
	Search    string    `bson:"search"`
	CreatedAt time.Time `bson:"createdAt"`
}

func main() {
	pgDB, err := core.ConnectPostgres()
	if err != nil {
		log.Println("Failed to connect to PostgreSQL:", err)
		time.Sleep(5 * time.Second)
	}

	mongoClient, err := core.ConnectMongo()
	if err != nil {
		log.Println("Failed to connect to MongoDB:", err)
		time.Sleep(5 * time.Second)
	}

	log.Println("Starting MongoDB to PostgreSQL migration...")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := migrateItems(mongoClient, pgDB); err != nil {
			log.Println("migrateItems failed:", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := migratePrices(mongoClient, pgDB); err != nil {
			log.Println("migratePrices failed:", err)
		}
	}()

	wg.Wait()
	log.Println("Iteration finished. Restarting in 5 seconds...")

	time.Sleep(1 * time.Minute)
}

func migrateItems(mongoClient *mongo.Client, pgDB *sql.DB) error {
	log.Println("Migrating items collection...")

	collection := mongoClient.Database("snapprice").Collection("items")
	cursor, err := collection.Find(context.Background(), bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	count := 0
	for cursor.Next(context.Background()) {
		var item MongoItem
		if err := cursor.Decode(&item); err != nil {
			log.Printf("Error decoding item: %v", err)
			continue
		}

		query := `
			INSERT INTO items (uuid, title, brand, link, source_name, image)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (uuid) DO UPDATE SET
				title = EXCLUDED.title,
				brand = EXCLUDED.brand,
				link = EXCLUDED.link,
				source_name = EXCLUDED.source_name,
				image = EXCLUDED.image,
				updated_at = CURRENT_TIMESTAMP
		`

		_, err := pgDB.Exec(query,
			item.ID.Hex(),
			item.Title,
			item.Brand,
			item.Link,
			item.Sources.Source,
			item.Images[0],
		)

		if err != nil {
			log.Printf("Error inserting item %s: %v", item.ID.Hex(), err)
			continue
		}

		count++
		if count%100 == 0 {
			log.Printf("Migrated %d items...", count)
		}
	}

	log.Printf("Successfully migrated %d items", count)
	return nil
}

func migratePrices(mongoClient *mongo.Client, pgDB *sql.DB) error {
	log.Println("Migrating prices collection...")

	collection := mongoClient.Database("snapprice").Collection("prices")
	cursor, err := collection.Find(context.Background(), bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(context.Background())

	count := 0
	for cursor.Next(context.Background()) {
		var price MongoPrice
		if err := cursor.Decode(&price); err != nil {
			log.Printf("Error decoding price: %v", err)
			continue
		}

		var totalCount int
		selectQuesry := `
			SELECT * FROM prices WHERE date = $1
		`

		err := pgDB.QueryRow(selectQuesry, price.Date).Scan(&totalCount)
		if err != nil {
			log.Print(err)
		}

		if totalCount == 0 {
			query := `
				INSERT INTO prices (item_id, price, date) VALUES ($1, $2, $3)
			`

			_, err := pgDB.Exec(query, price.ItemID, price.Price, price.Date)
			if err != nil {
				log.Printf("Error inserting price for item %s: %v", price.ItemID, err)
				continue
			}

			count++
			if count%100 == 0 {
				log.Printf("Migrated %d prices...", count)
			}
		} else {
			fmt.Println("count: ", totalCount)
		}
	}

	log.Printf("Successfully migrated %d prices", count)
	return nil
}
