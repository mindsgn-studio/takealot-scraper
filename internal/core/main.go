package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"firebase.google.com/go/v4/messaging"
	"github.com/PuerkitoBio/goquery"
	"github.com/appleboy/go-fcm"
	"github.com/chromedp/chromedp"
	"github.com/gocolly/colly"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/internal/model"
	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Item struct {
	ID            string   `json:"id"`
	UUID          string   `json:"uuid"`
	Link          string   `json:"link"`
	Brand         string   `json:"brand"`
	Image         string   `json:"image"`
	Images        []string `json:"images"`
	Title         string   `json:"title"`
	Source_Name   string   `json:"source_name"`
	Current_Price float64  `json:"current_price"`
}

type Prices struct {
	Item_ID string    `json:"item_id"`
	Price   float64   `json:"price"`
	Date    time.Time `json:"date"`
}

func ExtractTakealotID(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return ""
	}

	lastPart := parts[len(parts)-1] // "PLID93155744"

	if strings.HasPrefix(lastPart, "PLID") {
		return strings.TrimPrefix(lastPart, "PLID")
	}

	return ""
}

func SaveItemData(mongoClient *mongo.Client, title string, images []string, link string, id string, brand string) (primitive.ObjectID, error) {
	collection := mongoClient.Database("snapprice").Collection("prices")

	filter := bson.M{
		"sources.id":     id,
		"sources.source": "takealot",
	}
	update := bson.M{
		"$set": bson.M{
			"title":   title,
			"images":  images,
			"link":    link,
			"brand":   brand,
			"updated": time.Now().UTC(),
		},
		"$setOnInsert": bson.M{
			"created": time.Now().UTC(),
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var updatedDoc bson.M
	err := collection.FindOneAndUpdate(context.Background(), filter, update, opts).Decode(&updatedDoc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			var doc bson.M
			if err2 := collection.FindOne(context.Background(), filter).Decode(&doc); err2 == nil {
				updatedDoc = doc
			} else {
				return primitive.NilObjectID, fmt.Errorf("find after upsert failed: %w / %v", err, err2)
			}
		} else {
			return primitive.NilObjectID, fmt.Errorf("findoneandupdate: %w", err)
		}
	}

	if oid, ok := updatedDoc["_id"].(primitive.ObjectID); ok {
		return oid, nil
	}

	if idVal, ok := updatedDoc["_id"].(string); ok {
		oid, err := primitive.ObjectIDFromHex(idVal)
		if err == nil {
			return oid, nil
		}
	}
	return primitive.NilObjectID, errors.New("could not resolve item _id after upsert")
}

func GetCurrent(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	return prices[len(prices)-1].Price
}

func GetPrevious(prices []Prices) float64 {
	if len(prices) < 2 {
		return 0
	}
	return prices[len(prices)-2].Price
}

func LowestPrice(prices []Prices) float64 {
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

func HighestPrice(prices []Prices) float64 {
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

func AveragePrice(prices []Prices) float64 {
	if len(prices) == 0 {
		return 0
	}
	var total float64
	for _, p := range prices {
		total += p.Price
	}
	return total / float64(len(prices))
}

func PriceChange(prices []Prices) float64 {
	if len(prices) < 2 {
		return 0
	}

	current := GetCurrent(prices)
	previous := GetPrevious(prices)

	if previous == 0 {
		return 0
	}

	change := ((current - previous) / previous) * 100
	return math.Round(change*100) / 100
}

func SavePrice(mongoClient *mongo.Client, currentPrice float64, uuid string) {
	newObjectID, err := primitive.ObjectIDFromHex(uuid)
	if err != nil {
		fmt.Print(err.Error())
		return
	}

	collection := mongoClient.Database("snapprice").Collection("prices")
	doc := model.Price{
		ItemID:   newObjectID,
		Date:     time.Now().UTC(),
		Currency: "zar",
		Price:    currentPrice,
	}

	cursor, err := collection.InsertOne(context.Background(), doc)
	if err != nil {
		fmt.Print(err.Error())
		return
	}

	fmt.Println(cursor.InsertedID)
}

func AssessItem(pgDB *sql.DB, link string) ([]Item, error) {
	query := `SELECT link, uuid, source_name, title, image FROM items WHERE link = $1`

	rows, err := pgDB.Query(query, link)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Link, &item.UUID, &item.Source_Name, &item.Title, &item.Image); err != nil {
			log.Println("Error scanning price:", err)
		}

		fmt.Println(item)
	}

	return items, nil
}

func ExtractTakealotPrice(text string) float64 {
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

func ConnectPostgres() (*sql.DB, error) {
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

func ConnectMongo() (*mongo.Client, error) {
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

func ExtractText(text string) float64 {
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

func OpenAmazonPage(link string) float64 {
	var currentPrice float64
	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("body", func(body *colly.HTMLElement) {
		body.ForEach("div.a-section.a-spacing-none.aok-align-center.aok-relative", func(index int, element *colly.HTMLElement) {
			currentPrice = ExtractText(element.Text)
			return
		})
	})

	collyClient.Visit(link)
	collyClient.Wait()
	return currentPrice
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

func OpenPageTakealot(link string) (Item, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	var html string

	var item = Item{
		UUID:          "",
		Link:          link,
		Image:         "",
		Title:         "",
		Source_Name:   "",
		Current_Price: 0,
	}

	err := chromedp.Run(ctx,
		chromedp.Navigate(link),
		chromedp.Sleep(5*time.Second),
		chromedp.OuterHTML("html", &html),
	)

	if err != nil {
		log.Println("Error:", err)
		return item, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		log.Println("Error parsing HTML:", err)
		return item, err
	}

	price := strings.TrimSpace(
		doc.Find("span.currency.plus.currency-module_currency_29IIm").First().Text(),
	)

	currentPrice := extractTakealotPrice(price)

	title := doc.Find("h1").Text()

	brand := doc.Find("span.brand-link").Text()

	var images []string
	doc.Find("img").Each(func(index int, s *goquery.Selection) {
		dataRef, _ := s.Attr("data-ref")
		if strings.Contains(dataRef, "main-gallery-photo") {
			src, exists := s.Attr("src")
			if exists {
				images = append(images, src)
			}
		}
	})

	id := ExtractTakealotID(link)
	if id == "" {
		return item, fmt.Errorf("no id")
	}

	item.ID = id
	item.Current_Price = currentPrice
	item.Title = title
	item.Brand = brand
	item.Images = images
	item.Image = images[0]

	return item, nil
}

func IOSPushNotification(DeviceToken string) {
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

func AndroidpushhNotification(DeviceToken string) {
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
