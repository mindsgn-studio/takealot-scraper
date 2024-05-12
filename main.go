package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoClient *mongo.Client
var ctx context.Context

type JsonObject map[string]interface{}

func saveItemPrice(item map[string]interface{}) {
	fmt.Println(item)
}

func saveItemData(
	title string,
	images []string,
	brand string,
	link string,
	itemID string) {

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
			fmt.Println(imageUrl)
		}
	default:
		fmt.Println("Warning: Unexpected type for 'images' field in gallery")
		return nil, fmt.Errorf("unexpected type for gallery: %T", gallery)
	}

	return images, nil
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

	buySummary, ok := item["buybox_summary"].(map[string]interface{})
	if !ok {
		return nil
	}

	prices, ok := buySummary["prices"].([]string)
	if !ok {
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

	link := ""
	itemID := ""

	if title == "" || brand == "" || link == "" || itemID == "" {
		return nil
	}

	saveItemData(
		title,
		image,
		brand,
		link,
		itemID,
	)

	fmt.Println(title, brand, slug, prices)
	return nil
}

func getItems(brand string, nextIsAfter string) error {
	escapedBrand := url.QueryEscape(brand)
	url := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escapedBrand)

	if nextIsAfter != "" {
		url += "&after=" + nextIsAfter
	}

	fmt.Println(brand, nextIsAfter)

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
			// Handle the case where "product_views" is not a map
			fmt.Println("Missing or invalid 'product_views' field")
			continue
		}

		extractItemData(productViews)
	}

	time.Sleep(1 * time.Second)

	paging, ok := products["paging"].(map[string]interface{})

	if !ok {
		return fmt.Errorf("results not found in products")
	}

	if nextPage, ok := paging["next_is_after"].(string); ok {
		if nextPage == "" {
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
	fmt.Println("\n")
	return nil
}

func getBrand() error {
	db := mongoClient.Database("snapprice")
	collection := db.Collection("items")

	pipeline := mongo.Pipeline{
		{{"$group", bson.D{
			{"_id", "$brand"},
			{"count", bson.D{{"$sum", 1}}},
		}}},
		{{"$project", bson.D{
			{"_id", 0},
			{"brand", "$_id"},
			{"count", 1},
		}}},
		{{"$sample", bson.D{{"size", 1}}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("error creating aggregation cursor: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			fmt.Printf("error closing cursor: %v\n", err)
		}
	}()

	for cursor.Next(ctx) {
		var result bson.M
		if err := cursor.Decode(&result); err != nil {
			return fmt.Errorf("error decoding document: %w", err)
		}
		brand := result["brand"].(string)
		getItems(brand, "")
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("cursor error: %w", err)
	}

	return nil
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

	brandError := getBrand()
	if brandError != nil {
		panic(brandError)
	}
}
