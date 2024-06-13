package scraper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
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

	source := "takealot"
	api := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/product-details/PLID%s?platform=desktop&display_credit=true", itemID)
	sqlStatement := `
	INSERT INTO items (title, image, brand, link, item_id, price, category, source, api)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (item_id) DO UPDATE
	SET title = excluded.title,
		image = excluded.image,
		brand = excluded.brand,
		link = excluded.link,
		category = excluded.category,
		source = excluded.source,
		api = CASE WHEN items.item_id = excluded.item_id THEN excluded.api ELSE items.api END,
		price = CASE WHEN items.item_id = excluded.item_id THEN excluded.price ELSE items.price END`

	defer db.Close()

	_, err := db.Exec(sqlStatement, title, images[0], brand, link, itemID, price, category, source, api)
	if err != nil {
		log.Fatalf("Error inserting into items table: %v", err)
	}

	fmt.Println(title, "saved!")
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
				continue
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

func extractItemData(item map[string]interface{}, category string) error {
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

	saveItemData(title, image, brand, link, itemID, price, category)
	total++

	return nil
}

func getItems(category string, nextIsAfter string) error {
	escapedBrand := url.QueryEscape(category)
	url := fmt.Sprintf("https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escapedBrand)

	if nextIsAfter != "" {
		url += "&after=" + nextIsAfter
	}

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Error: API returned status code %d", response.StatusCode)
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var data JsonObject
	if err := json.Unmarshal(body, &data); err != nil {
		return err
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

		extractItemData(productViews, category)
	}

	paging, ok := products["paging"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("results not found in products")
	}

	if nextPage, ok := paging["next_is_after"].(string); ok && nextPage != "" {
		return getItems(category, nextPage)
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
