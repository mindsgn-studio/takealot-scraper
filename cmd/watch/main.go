package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"firebase.google.com/go/v4/messaging"
	fcm "github.com/appleboy/go-fcm"
	"github.com/gocolly/colly"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
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

func main() {
	log.Println("Starting MongoDB to PostgreSQL migration...")

	pgDB, err := connectPostgres()
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}
	defer pgDB.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := getList(pgDB); err != nil {
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

func getCurrent(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	return prices[len(prices)-1].Price
}

func getPrevious(prices []Prices) float64 {
	if len(prices) < 2 {
		return 0
	}
	return prices[len(prices)-2].Price
}

func lowestPrice(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	lowest := prices[0].Price
	for _, p := range prices {
		if p.Price < lowest {
			lowest = p.Price
		}
	}
	return lowest
}

func highestPrice(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	highest := prices[0].Price
	for _, p := range prices {
		if p.Price > highest {
			highest = p.Price
		}
	}
	return highest
}

func averagePrice(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	var total float64
	for _, p := range prices {
		total += p.Price
	}
	return total / float64(len(prices))
}

func priceChange(prices []Prices) float64 {
	if len(prices) < 2 {
		return 0
	}

	current := getCurrent(prices)
	previous := getPrevious(prices)

	if previous == 0 {
		return 0
	}

	change := ((current - previous) / previous) * 100
	return math.Round(change*100) / 100
}

func androidpushhNotification(DeviceToken string) {
	fmt.Println(DeviceToken)
	ctx := context.Background()
	client, err := fcm.NewClient(
		ctx,
		fcm.WithCredentialsFile("./google-services.json"),
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Send(
		ctx,
		&messaging.Message{
			Token: DeviceToken,
			Data: map[string]string{
				"foo": "bar",
			},
		},
	)
	if err != nil {
		fmt.Println("Send error:", err)
		return
	}
	fmt.Println("Success:", resp.SuccessCount, "Failure:", resp.FailureCount)
}

func iosPushNotification(DeviceToken string) {
	authKey, err := token.AuthKeyFromFile("./AuthKey_CCKC4GS5P8.p8")
	if err != nil {
		log.Fatal("token error:", err)
	}

	token := &token.Token{
		AuthKey: authKey,
		KeyID:   "CCKC4GS5P8",
		TeamID:  "B3U8UM2966",
	}

	client := apns2.NewTokenClient(token)
	notification := &apns2.Notification{}
	notification.DeviceToken = DeviceToken
	notification.Topic = "mindsgn.studio.snap-price"
	notification.Payload = []byte(`{"aps":{"alert":"Hello!"}}`)

	res, err := client.Push(notification)

	if err != nil {
		log.Fatal("Error:", err)
	}

	fmt.Printf("%v %v %v\n", res.StatusCode, res.ApnsID, res.Reason)
}

func analyse(pgDB *sql.DB, currentPrice float64, uuid string) {
	query := `SELECT item_id, price, date FROM prices WHERE item_id = $1 ORDER BY date ASC`

	rows, err := pgDB.Query(query, uuid)
	if err != nil {
		log.Printf(err.Error())
	}
	defer rows.Close()

	var prices []Prices
	for rows.Next() {
		var price Prices
		if err := rows.Scan(&price.Item_ID, &price.Price, &price.Date); err != nil {
			log.Println("Error scanning price:", err)
		}
		prices = append(prices, price)

		if len(prices) == 0 {
			log.Println("No prices found for item:", uuid)
			return
		}
	}

	fmt.Println("Current Price:", getCurrent(prices))
	fmt.Println("Previous Price:", getPrevious(prices))
	fmt.Println("Lowest Price:", lowestPrice(prices))
	fmt.Println("Highest Price:", highestPrice(prices))
	fmt.Println("Average Price:", averagePrice(prices))
	fmt.Println("Price Change (%):", priceChange(prices))
}

func savePrice(pgDB *sql.DB, currentPrice float64, uuid string) {
	insertQuery := `INSERT INTO prices (item_id, price, date) VALUES ($1, $2, $3)`

	result, err := pgDB.Exec(insertQuery, uuid, currentPrice, time.Now())
	if err != nil {
		log.Printf("Error inserting price for item %s: %v", uuid, err)
	}

	fmt.Println(result.RowsAffected())
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

func OpenPageTakealot(pgDB *sql.DB, link string, uuid string) {}

func assessItem(pgDB *sql.DB, uuid string) {
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
			OpenPageTakealot(pgDB, item.Link, uuid)
		} else if item.Source_Name == "amazon" {
			OpenPageAmazon(pgDB, item.Link, uuid)
		}
	}
}

func getList(pgDB *sql.DB) error {
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

		assessItem(pgDB, watch.Item_ID.String)

		/*
			if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "ios" {
				iosPushNotification(watch.Token.String)
			}

			if watch.Token.Valid && watch.Device.Valid && watch.Device.String == "android" {
				androidpushhNotification(watch.Token.String)
			}
		*/
	}

	return nil
}
