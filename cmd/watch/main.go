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
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
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
	logger := log.New(os.Stdout, "[watch] ", log.LstdFlags|log.Lmsgprefix)
	log.Println("Starting MongoDB to PostgreSQL migration...")
	mongoClient, err := connectMongo()
	if err != nil {
		logger.Fatal("Failed to connect to MongoDB:", err)
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

	fmt.Println(cursor.InsertedID)
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
		cleanedInput := strings.ReplaceAll(price, ",", ".")
		fmtPrice, err := strconv.ParseFloat(cleanedInput, 64)
		if err != nil {
			fmt.Println(err)
			return 0
		}

		return fmtPrice
	}
	return 0
}

func extractTakealotPrice(text string) float64 {
	re := regexp.MustCompile("R[\\s\\p{Zs}]*([\\d.,\\s\\xA0]+)")
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return 0
	}

	priceStr := matches[1]

	priceStr = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, priceStr)

	priceStr = strings.ReplaceAll(priceStr, ",", "")

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		fmt.Println("Error parsing price:", err)
		return 0
	}

	return price
}

func OpenPageTakealot(mongoClient *mongo.Client, link string, uuid string) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	var html string

	err := chromedp.Run(ctx,
		chromedp.Navigate(link),
		chromedp.Sleep(5*time.Second),
		chromedp.OuterHTML("html", &html),
	)

	if err != nil {
		log.Println("Error:", err)
		return
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		log.Println("Error parsing HTML:", err)
		return
	}

	title := strings.TrimSpace(
		doc.Find("span.currency.plus.currency-module_currency_29IIm").First().Text(),
	)

	if title == "" {
		OpenPageTakealot(mongoClient, link, uuid)
		return
	}

	currentPrice := extractTakealotPrice(title)
	savePrice(mongoClient, currentPrice, uuid)
}

func OpenAmazonPage(mongoClient *mongo.Client, link string, uuid string) {
	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("body", func(body *colly.HTMLElement) {
		var currentPrice float64

		body.ForEach("div.a-section.a-spacing-none.aok-align-center.aok-relative", func(index int, element *colly.HTMLElement) {
			currentPrice = extractText(element.Text)
		})

		fmt.Println(link)
		savePrice(mongoClient, currentPrice, uuid)
	})

	collyClient.Visit(link)
	collyClient.Wait()
}

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
			OpenPageTakealot(mongoClient, item.Link, item.UUID)
		}

		if item.Source_Name == "amazon" {
			OpenPageTakealot(mongoClient, item.Link, item.UUID)
		}
	}

	return nil
}
