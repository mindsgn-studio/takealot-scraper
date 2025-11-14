package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/internal/model"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Watch struct {
	Item_ID sql.NullString `json:"item_id"`
	Token   sql.NullString `json:"token`
	Device  sql.NullString `json:"device`
}

type Item struct {
	UUID        string `json:"uuid"`
	Link        string `json:"link"`
	Source_Name string `json:"source_name"`
}

type Prices struct {
	Item_ID string    `json:"item_id"`
	Price   float64   `json:"price"`
	Date    time.Time `json:"date"`
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

func main() {
	log.Println("Starting MongoDB to PostgreSQL migration...")
	mongoClient, err := connectMongo()
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}
	defer mongoClient.Disconnect(context.Background())

	pgDB, err := connectPostgres()
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}
	defer pgDB.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := assessItem(pgDB, mongoClient); err != nil {
			log.Println("migrateItems failed:", err)
		}
	}()

	wg.Wait()

	log.Println("Migration completed successfully!")
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

func savePrice(mongoClient *mongo.Client, currentPrice float64, uuid string) {

	newObjectID, err := primitive.ObjectIDFromHex(uuid)
	if err != nil {
		fmt.Printf(err.Error())
		return
	}

	doc := model.Price{
		ItemID:   newObjectID,
		Date:     time.Now().UTC(),
		Currency: "zar",
		Price:    currentPrice,
	}
	collection := mongoClient.Database("snapprice").Collection("prices")

	cursor, err := collection.InsertOne(context.Background(), doc)
	if err != nil {
		fmt.Printf(err.Error())
		return
	}

	fmt.Println(cursor)
}

func extractText(text string) float64 {
	re := regexp.MustCompile(`R[\s\p{Zs}]*([\d\s\p{Zs}]+,\d{2})`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		price := matches[1]
		price = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\u00A0' {
				return -1
			}
			return r
		}, price)
		fmtPrice, err := strconv.ParseFloat(price, 64)
		if err != nil {
			return 0
		}

		return fmtPrice
	}
	return 0
}

func OpenPageAmazon(pgDB *sql.DB, mongoClient *mongo.Client, link string, uuid string) {
	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("body", func(body *colly.HTMLElement) {
		body.ForEach("div.a-section.a-spacing-none.aok-align-center.aok-relative", func(index int, element *colly.HTMLElement) {
			currentPrice := extractText(element.Text)
			savePrice(mongoClient, currentPrice, uuid)
		})
	})

	collyClient.Visit(link)
	collyClient.Wait()
}

func OpenPageTakealot(pgDB *sql.DB, mongoClient *mongo.Client, link string, uuid string) {}

func assessItem(pgDB *sql.DB, mongoClient *mongo.Client) error {
	query := `SELECT link, uuid, source_name FROM items`

	rows, err := pgDB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Link, &item.UUID, &item.Source_Name); err != nil {
			log.Println("Error scanning price:", err)
		}

		if item.Source_Name == "takealot" {
			OpenPageTakealot(pgDB, mongoClient, item.Link, item.UUID)
		} else if item.Source_Name == "amazon" {
			OpenPageAmazon(pgDB, mongoClient, item.Link, item.UUID)
		}
	}

	return nil
}
