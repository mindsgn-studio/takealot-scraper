package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type JsonObject map[string]interface{}

var mongoClient *mongo.Client
var ctx context.Context
var total uint64 = 0
var page uint64 = 1
var items uint64 = 0

type Price struct {
	ItemID   string    `bson:"itemID"`
	Date     time.Time `bson:"date"`
	Currency string    `bson:"currency"`
	Price    float64   `bson:"price"`
}

func connectDatabase() error {
	ctx = context.Background()
	err := godotenv.Load()
	if err != nil {
		return fmt.Errorf("error loading .env file: %w", err)
	}

	mongoURI := os.Getenv("MONGODB_URI")
	mongoClient, err = mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("error connecting to MongoDB: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err = mongoClient.Ping(ctx, nil)
	if err != nil {
		return fmt.Errorf("error pinging MongoDB: %w", err)
	}

	fmt.Println("Connected to MongoDB successfully")
	return nil
}

func randomSleep() {
	seconds := rand.Intn(10) + 1
	time.Sleep(time.Duration(seconds) * time.Second)
}

func saveItemPrice(price float64, title string, link string) {
	db := mongoClient.Database("snapprice")
	itemCollection := db.Collection("items")
	pricesCollection := db.Collection("prices")

	twoHoursAgo := time.Now().Add(-2 * time.Hour)

	filter := map[string]interface{}{
		"title": title,
		"link":  link,
	}

	var result map[string]interface{}

	err := itemCollection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return
	}

	if id, ok := result["_id"].(primitive.ObjectID); ok {
		itemID := id.Hex()
		filter := map[string]interface{}{
			"itemID": itemID,
			"date":   map[string]interface{}{"$gt": twoHoursAgo},
		}

		var result map[string]interface{}
		err := pricesCollection.FindOne(ctx, filter).Decode(&result)
		if err != nil {
			newPrice := &Price{
				ItemID:   itemID,
				Date:     time.Now(),
				Currency: "zar",
				Price:    price,
			}

			_, err := pricesCollection.InsertOne(ctx, newPrice)
			if err != nil {
				fmt.Println("failed to save Price")
				return
			}
		}
	}

	return
}

func saveItemData(title string, images []string, link string, id string, brand string) {
	db := mongoClient.Database("snapprice")
	collection := db.Collection("items")

	var filter = map[string]interface{}{
		"sources.id": id,
	}

	var update = map[string]interface{}{
		"$set": bson.M{
			"title":   title,
			"images":  images,
			"link":    link,
			"brand":   brand,
			"updated": time.Now(),
			"sources": bson.M{
				"id":     id,
				"source": "takealot",
			},
		},
	}

	upsert := true

	_, err := collection.UpdateOne(ctx, filter, update, &options.UpdateOptions{Upsert: &upsert})
	if err != nil {
		fmt.Println(err)
		return
	}
}

func extractPrice(prices interface{}) (float64, error) {
	switch prices := prices.(type) {
	case []interface{}:
		if len(prices) == 0 {
			return 0, errors.New("Error: Empty slice provided for prices")
		}

		if price, ok := prices[0].(float64); ok {
			return price, nil
		}
		return 0, errors.New("Error: Invalid price format in prices")
	default:
		return 0, errors.New("Error: Invalid price format in prices")
	}
}

func extractImage(gallery interface{}) ([]string, error) {
	switch gallery := gallery.(type) {
	case string:
		fmt.Println("Warning: 'images' field in gallery is a single string, expected an array.")
		return nil, fmt.Errorf("unexpected type for gallery: string")
	case []interface{}:
		images := make([]string, 0, len(gallery))
		for _, imageInterface := range gallery {
			if image, ok := imageInterface.(string); ok {
				imageURL := strings.ReplaceAll(image, "{size}", "zoom")
				images = append(images, imageURL)
			} else {
				fmt.Println("Error: Invalid image format in gallery")
			}
		}
		return images, nil
	default:
		fmt.Println("Warning: Unexpected type for 'images' field in gallery")
		return nil, fmt.Errorf("unexpected type for gallery: %T", gallery)
	}
}

func getProductID(products interface{}) (string, error) {
	switch products := products.(type) {
	case []interface{}:
		for _, product := range products {
			if productMap, ok := product.(map[string]interface{}); ok {
				if id, ok := productMap["id"].(string); ok {
					return id, nil
				}
			}
		}
	case map[string]interface{}:
		if id, ok := products["id"].(string); ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("ID not found in product data")
}

func extractItemData(item map[string]interface{}, category string) error {
	core, coreOK := item["core"].(map[string]interface{})
	gallery, galleryOK := item["gallery"].(map[string]interface{})
	buySummary, buySummaryOK := item["buybox_summary"].(map[string]interface{})
	enhancedEcommerceClick, enhancedEcommerceClickOK := item["enhanced_ecommerce_click"].(map[string]interface{})

	if !coreOK || !galleryOK || !buySummaryOK || !enhancedEcommerceClickOK {
		return nil
	}

	images, imagesOK := gallery["images"]
	if !imagesOK {
		return nil
	}

	image, err := extractImage(images)
	if err != nil {
		return nil
	}

	title, titleOK := core["title"].(string)
	brand, brandOK := core["brand"].(string)
	slug, slugOK := core["slug"].(string)

	if !titleOK || !brandOK || !slugOK {
		return nil
	}

	ecommerce, ecommerceOK := enhancedEcommerceClick["ecommerce"].(map[string]interface{})
	if !ecommerceOK {
		return nil
	}

	click, clickOK := ecommerce["click"].(map[string]interface{})
	if !clickOK {
		return nil
	}

	products, productsOK := click["products"]
	if !productsOK {
		return nil
	}

	id, err := getProductID(products)
	if err != nil {
		return nil
	}

	link := fmt.Sprintf("https://www.takealot.com/%s/%s", slug, strings.ReplaceAll(id, "PLID", ""))
	prices, pricesOK := buySummary["prices"]

	if !pricesOK {
		return nil
	}

	price, err := extractPrice(prices)
	if err != nil {
		return nil
	}

	if title == "" || brand == "" || link == "" {
		return nil
	}

	var plid = strings.ReplaceAll(id, "PLID", "")
	saveItemData(title, image, link, plid, brand)
	saveItemPrice(price, title, link)
	total++
	items++

	return nil
}

func getItems(item string, nextIsAfter string) {
	escapedItem := url.QueryEscape(item)
	apiURL := fmt.Sprintf("https://api.takealot.com/rest/v-1-14-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escapedItem)

	if nextIsAfter != "" {
		apiURL += "&after=" + nextIsAfter
	}

	request, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		fmt.Errorf("failed to create HTTP request: %v", err)
		return
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Errorf("failed to execute HTTP request: %v", err)
		return
	}

	defer func() {
		// Close the response body before exiting the function
		if err := response.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	if response.StatusCode != http.StatusOK {
		fmt.Errorf("API returned non-OK status code: %d", response.StatusCode)
		return
	}

	decoder := json.NewDecoder(response.Body)
	var data JsonObject
	if err := decoder.Decode(&data); err != nil {
		fmt.Errorf("failed to decode JSON: %v", err)
		return
	}

	sections, ok := data["sections"].(map[string]interface{})
	if !ok {
		fmt.Errorf("sections not found in response")
		return
	}

	products, ok := sections["products"].(map[string]interface{})
	if !ok {
		fmt.Errorf("products not found in sections")
		return
	}

	results, ok := products["results"].([]interface{})
	if !ok {
		fmt.Errorf("results not found in products")
		return
	}

	for _, result := range results {
		productMap, ok := result.(map[string]interface{})

		if !ok {
			fmt.Println("Invalid product format")
			continue
		}

		productViews, ok := productMap["product_views"].(map[string]interface{})
		if !ok {
			fmt.Println("Missing or invalid 'product_views' field")
			continue
		}

		if err := extractItemData(productViews, item); err != nil {
			fmt.Printf("Error extracting item data: %v\n", err)
		}
	}

	paging, ok := products["paging"].(map[string]interface{})
	if !ok {
		fmt.Errorf("paging not found in products")
		return
	}

	nextIsAfter, nextPageExists := paging["next_is_after"].(string)
	if nextPageExists && nextIsAfter != "" {
		page++
		fmt.Print("items: ", items, "\tpage: ", page)
		fmt.Print("\n")
		items = 0
		getItems(item, nextIsAfter)
	}

	fmt.Println("Total Items:", total)
	fmt.Print("\n")
	fmt.Print("\n")
	// randomSleep()
	total = 0
	getBrand()
	return
}

func getBrand() {
	data, err := ioutil.ReadFile("brand.txt")
	if err != nil {
		fmt.Println("Error reading brand.txt:", err)
		return
	}

	rawBrands := strings.Split(string(data), ",")
	var brands []string
	for _, b := range rawBrands {
		trimmed := strings.TrimSpace(b)
		if trimmed != "" {
			brands = append(brands, trimmed)
		}
	}
	if len(brands) == 0 {
		fmt.Println("No brands found in brand.txt")
		return
	}
	rand.Seed(time.Now().UnixNano())
	brand := brands[rand.Intn(len(brands))]
	fmt.Println("================================")
	fmt.Println(brand)
	fmt.Println("================================")
	page = 0
	items = 0
	getItems(brand, "")
}

func main() {
	err := connectDatabase()
	if err != nil {
		panic(err)
	}

	getBrand()
}
