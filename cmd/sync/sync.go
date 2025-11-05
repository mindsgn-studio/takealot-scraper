package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	log.Println("Starting MongoDB to PostgreSQL migration...")

	pgDB, err := connectPostgres()
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}
	defer pgDB.Close()

	mongoClient, err := connectMongo()
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}
	defer mongoClient.Disconnect(context.Background())

	if err := migrateItems(mongoClient, pgDB); err != nil {
		log.Printf("Error migrating items: %v", err)
	}

	if err := migratePrices(mongoClient, pgDB); err != nil {
		log.Printf("Error migrating prices: %v", err)
	}

	log.Println("Migration completed successfully!")
}

func connectMongo() (*mongo.Client, error) {
	_ = godotenv.Load()
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		return nil, fmt.Errorf("MONGO environment variable not set")
	}

	clientOptions := options.Client().
		ApplyURI(mongoURI).
		SetConnectTimeout(30 * time.Second).
		SetServerSelectionTimeout(30 * time.Second).
		SetMaxPoolSize(10).
		SetMinPoolSize(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Println("Connected to MongoDB")
	return client, nil
}

func connectPostgres() (*sql.DB, error) {
	_ = godotenv.Load()
	pgURI := os.Getenv("POSTGRES_URI")
	if pgURI == "" {
		return nil, fmt.Errorf("POSTGRES_URI environment variable not set")
	}

	db, err := sql.Open("postgres", pgURI)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	log.Println("Connected to PostgreSQL")
	return db, nil
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

		imagesStr := strings.Join(item.Images, ",")

		query := `
			INSERT INTO items (uuid, title, brand, link, source_name, images)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (uuid) DO UPDATE SET
				title = EXCLUDED.title,
				brand = EXCLUDED.brand,
				link = EXCLUDED.link,
				source_name = EXCLUDED.source_name,
				images = EXCLUDED.images,
				updated_at = CURRENT_TIMESTAMP
		`

		_, err := pgDB.Exec(query,
			item.ID.Hex(),
			item.Title,
			item.Brand,
			item.Link,
			item.Sources.Source,
			imagesStr,
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

		query := `
			INSERT INTO prices (item_id, price, date)
			VALUES ($1, $2, $3)
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
	}

	log.Printf("Successfully migrated %d prices", count)
	return nil
}
