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

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/internal/core"
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
		if err := getList(pgDB, mongoClient); err != nil {
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

func analyse(pgDB *sql.DB, currentPrice float64, uuid string) {
	query := `SELECT item_id, price, date FROM prices WHERE item_id = $1 ORDER BY date ASC`

	rows, err := pgDB.Query(query, uuid)
	if err != nil {
		log.Printf(err.Error())
	}
	defer rows.Close()

	var prices []core.Prices
	for rows.Next() {
		var price core.Prices
		if err := rows.Scan(&price.Item_ID, &price.Price, &price.Date); err != nil {
			log.Println("Error scanning price:", err)
		}
		prices = append(prices, price)

		if len(prices) == 0 {
			log.Println("No prices found for item:", uuid)
			return
		}
	}

	fmt.Println("Current Price:", core.GetCurrent(prices))
	fmt.Println("Previous Price:", core.GetPrevious(prices))
	fmt.Println("Lowest Price:", core.LowestPrice(prices))
	fmt.Println("Highest Price:", core.HighestPrice(prices))
	fmt.Println("Average Price:", core.AveragePrice(prices))
	fmt.Println("Price Change (%):", core.PriceChange(prices))
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

/*
func OpenPageAmazon(pgDB *sql.DB, link string, uuid string) {
	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("body", func(body *colly.HTMLElement) {
		body.ForEach("div.a-section.a-spacing-none.aok-align-center.aok-relative", func(index int, element *colly.HTMLElement) {
			currentPrice := extractText(element.Text)
			savePrice(pgDB, currentPrice, uuid)
			analyse(pgDB, currentPrice, uuid)
		})
	})

	collyClient.Visit(link)
	collyClient.Wait()
}
*/

func OpenPageTakealot(pgDB *sql.DB, mongoClient *mongo.Client, link string, uuid string) {
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
		OpenPageTakealot(pgDB, mongoClient, link, uuid)
		return
	}

	currentPrice := core.ExtractTakealotPrice(title)
	core.SavePrice(mongoClient, currentPrice, uuid)
}

func assessItem(pgDB *sql.DB, mongoClient *mongo.Client, uuid string) {
	query := `SELECT link, uuid, source_name FROM items WHERE uuid = $1`

	rows, err := pgDB.Query(query, uuid)
	if err != nil {
		log.Printf(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Link, &item.UUID, &item.Source_Name); err != nil {
			log.Println("Error scanning price:", err)
		}

		if item.Source_Name == "takealot" {
			OpenPageTakealot(pgDB, mongoClient, item.Link, uuid)
		}
	}
}

func getList(pgDB *sql.DB, mongoClient *mongo.Client) error {
	query := `
		SELECT item_id, token, device FROM watch
	`

	rows, err := pgDB.Query(query)
	if err != nil {
		fmt.Printf(err.Error())
	}
	defer rows.Close()

	for rows.Next() {
		var watch Watch
		if err := rows.Scan(&watch.Item_ID, &watch.Token, &watch.Device); err != nil {
			log.Println("Error scanning item:", err)
			continue
		}

		assessItem(pgDB, mongoClient, watch.Item_ID.String)

		/*
			if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "ios" {
				iosPushNotification(watch.Token.String)
			}

			if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "android" {
				androidpushhNotification(watch.Token.String)
			}
		*/
	}

	getList(pgDB, mongoClient)
	return nil
}
