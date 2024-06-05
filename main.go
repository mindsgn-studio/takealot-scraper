package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/mindsgn-studio/takealot-scraper/category"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoClient *mongo.Client
var ctx context.Context
var total uint64 = 0

type JsonObject map[string]interface{}

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
		"title": title,
		"brand": brand,
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
			"date":   map[string]interface{}{"$gt": twelveHoursAgo},
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
				fmt.Println(err)
				return
			}
		}
	}

	return
}

func saveItemData(
	title string,
	images []string,
	brand string,
	link string,
	itemID string) {

	db := mongoClient.Database("snapprice")
	collection := db.Collection("items")

	var filter = map[string]interface{}{
		"sources.id": itemID,
	}

	var update = map[string]interface{}{
		"$set": map[string]interface{}{
			"title":   title,
			"images":  images,
			"brand":   brand,
			"link":    link,
			"updated": time.Now(),
			"sources": map[string]interface{}{
				"id":     itemID,
				"source": "takealot",
				"api":    `https://api.takealot.com/rest/v-1-11-0/product-details/PLID` + string(itemID) + "?platform=desktop&display_credit=true",
			},
		},
	}

	upsert := true

	_, err := collection.UpdateOne(ctx, filter, update, &options.UpdateOptions{Upsert: &upsert})
	if err != nil {
		print(err)
		return
	}
}

func extractPrice(prices interface{}) (float64, error) {
	switch prices.(type) {
	case []interface{}:
		if len(prices.([]interface{})) == 0 {
			return 0, errors.New("Error: Empty slice provided for prices")
		}

		price, ok := prices.([]interface{})[0].(float64)
		if !ok {
			return 0, errors.New("Error: Invalid price format in prices")
		}

		return price, nil
	default:
		return 0, fmt.Errorf("Error: Invalid [rice format in prices")
	}
}

func extractImage(gallery interface{}) ([]string, error) {
	var images []string

	switch gallery.(type) {
	case string:
		fmt.Println("Warning: 'images' field in gallery is a single string, expected an array.")
		return nil, fmt.Errorf("unexpected type for gallery: string")
	case []interface{}:
		for _, imageInterface := range gallery.([]interface{}) {
			image, ok := imageInterface.(string)
			if !ok {
				fmt.Println("Error: Invalid image format in gallery")
				continue
			}
			imageUrl := strings.ReplaceAll(image, "{size}", "zoom")
			images = append(images, imageUrl)
		}
	default:
		fmt.Println("Warning: Unexpected type for 'images' field in gallery")
		return nil, fmt.Errorf("unexpected type for gallery: %T", gallery)
	}

	return images, nil
}

func getProductID(products interface{}) (string, error) {
	switch products.(type) {
	case []interface{}:
		for _, productInterface := range products.([]interface{}) {
			product, ok := productInterface.(map[string]interface{})
			if !ok {
				continue // Skip invalid entries
			}
			id, ok := product["id"].(string)
			if ok {
				return id, nil
			}
		}
	case map[string]interface{}:
		id, ok := products.(map[string]interface{})["id"].(string)
		if ok {
			return id, nil
		}

	default:
		return "", fmt.Errorf("unexpected type for products: %T", products)
	}
	return "", fmt.Errorf("ID not found in product data")
}

func extractItemData(item map[string]interface{}) error {
	core, ok := item["core"].(map[string]interface{})
	if !ok {
		return nil
	}

	gallery, ok := item["gallery"].(map[string]interface{})
	if !ok {
		return nil
	}

	images, ok := gallery["images"]
	if !ok {
		return nil
	}

	image, err := extractImage(images)
	if err != nil {
		return nil
	}

	title, ok := core["title"].(string)
	if !ok {
		return nil
	}

	brand, ok := core["brand"].(string)
	if !ok {
		return nil
	}

	slug, ok := core["slug"].(string)
	if !ok {
		return nil
	}

	buySummary, ok := item["buybox_summary"].(map[string]interface{})
	if !ok {
		return nil
	}

	enhancedEcommerceClick, ok := item["enhanced_ecommerce_click"].(map[string]interface{})
	if !ok {
		return nil
	}

	ecommerce, ok := enhancedEcommerceClick["ecommerce"].(map[string]interface{})
	if !ok {
		return nil
	}

	click, ok := ecommerce["click"].(map[string]interface{})
	if !ok {
		return nil
	}

	products, ok := click["products"]
	if !ok {
		return nil
	}

	id, err := getProductID(products)

	if err != nil {
		return nil
	}

	link := fmt.Sprintf("https://www.takealot.com/%s/%s", slug, id)
	itemID := strings.ReplaceAll(id, "PLID", "")
	prices, ok := buySummary["prices"]

	if !ok {
		return nil
	}

	price, err := extractPrice(prices)
	if err != nil {
		return nil
	}

	if title == "" || brand == "" || link == "" {
		return nil
	}

	saveItemData(
		title,
		image,
		brand,
		link,
		itemID,
	)
	saveItemPrice(price, title, brand, link)
	total++

	return nil
}

func getItems(brand string, nextIsAfter string) error {
	fmt.Println(brand)
	escapedBrand := url.QueryEscape(brand)
	url := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escapedBrand)

	if nextIsAfter != "" {
		url += "&after=" + nextIsAfter
	}

	client := &http.Client{}

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	response, err := client.Do(request)
	if err != nil {
		return nil
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		fmt.Printf("Error: API return status code %d\n", response.StatusCode)
		return nil
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	var data JsonObject
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	sections, ok := data["sections"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("sections not found in response")
	}

	products, ok := sections["products"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("products not found in sections")
	}

	results, ok := products["results"].([]interface{})
	if !ok {
		return fmt.Errorf("results not found in products")
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

		extractItemData(productViews)
	}

	paging, ok := products["paging"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("results not found in products")
	}

	if nextPage, ok := paging["next_is_after"].(string); ok {
		if nextPage == "" {
			fmt.Println("total Items:", total)
			total = 0
			getBrand()
		}

		getItems(brand, nextPage)
	}

	return nil
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

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = mongoClient.Ping(ctx, nil)
	if err != nil {
		return fmt.Errorf("error pinging MongoDB: %w", err)
	}

	fmt.Println("Connected to MongoDB successfully")
	return nil
}

func getBrand() {
	brand := category.GetRandomCategory()
	getItems(brand, "")
}

func main() {
	defer func() {
		if err := mongoClient.Disconnect(ctx); err != nil {
			fmt.Printf("error disconnecting from MongoDB: %v\n", err)
		}
	}()

	connectionErr := connectDatabase()
	if connectionErr != nil {
		panic(connectionErr)
	}

	getBrand()
}
