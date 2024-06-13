package scraper

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/mindsgn-studio/takealot-scraper/category"
	"github.com/mindsgn-studio/takealot-scraper/database"
)

var total uint64 = 0

type JsonObject map[string]interface{}

var client = &http.Client{}

type Price struct {
	ItemID   string    `bson:"ItemID"`
	Date     time.Time `bson:"date"`
	Currency string    `bson:"currency"`
	Price    float64   `bson:"price"`
}

func saveItemData(title string, images []string, brand string, link string, itemID string, price float64, category string) {
	db := database.ConnectDatabase()
	defer db.Close()

	source := "takealot"
	api := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/product-details/PLID%s?platform=desktop&display_credit=true", itemID)

	sqlStatement := `
		INSERT INTO items (title, image, brand, link, item_id, price, category, source, api)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (item_id) DO UPDATE
		SET title = EXCLUDED.title,
			image = EXCLUDED.image,
			brand = EXCLUDED.brand,
			link = EXCLUDED.link,
			category = EXCLUDED.category,
			source = EXCLUDED.source,
			api = CASE WHEN items.item_id = EXCLUDED.item_id THEN EXCLUDED.api ELSE items.api END,
			price = CASE WHEN items.item_id = EXCLUDED.item_id THEN EXCLUDED.price ELSE items.price END`

	_, err := db.Exec(sqlStatement, title, images[0], brand, link, itemID, price, category, source, api)
	if err != nil {
		log.Printf("Error inserting into items table: %v", err)
		return
	}

	fmt.Println(title, "saved!")
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
		images := make([]string, 0, len(gallery)) // Preallocate slice capacity
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
		return nil // Missing essential data, return early
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

	saveItemData(title, image, brand, link, strings.ReplaceAll(id, "PLID", ""), price, category)
	total++

	return nil
}

func getItems(category string, nextIsAfter string) error {
	escapedCategory := url.QueryEscape(category)
	apiURL := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escapedCategory)

	if nextIsAfter != "" {
		apiURL += "&after=" + nextIsAfter
	}

	request, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned non-OK status code: %d", response.StatusCode)
	}

	decoder := json.NewDecoder(response.Body)
	var data JsonObject
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("failed to decode JSON: %v", err)
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

		if err := extractItemData(productViews, category); err != nil {
			fmt.Printf("Error extracting item data: %v\n", err)
		}
	}

	paging, ok := products["paging"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("paging not found in products")
	}

	nextIsAfter, nextPageExists := paging["next_is_after"].(string)
	if nextPageExists && nextIsAfter != "" {
		return getItems(category, nextIsAfter)
	}

	fmt.Println("Total Items:", total)
	total = 0
	GetBrand()
	return nil
}

func GetBrand() {
	category := category.GetRandomCategory()
	fmt.Println("======================================================================")
	fmt.Println("Category:", category)
	fmt.Println("======================================================================")
	getItems(category, "")
}
