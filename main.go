package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoUri string
	baseURL string
	words []string
	after string
	isPaged bool = false
	connectedToDatabase bool = true
	mongoClient *mongo.Client
	mongoDatabase string
	mongoCollection string
)

type ApiResponse struct {
	Sections map[string]Section `json:"sections"`
}

type Section struct {
	Name               string            `json:"name"`
	ID                 string            `json:"id"`
	Paging             Paging            `json:"paging"`
	IsPaged            bool              `json:"is_paged"`
	SearchRequestID    string            `json:"search_request_id"`
}

type Paging struct {
	NextIsAfter      string `json:"next_is_after"`
	PreviousIsBefore string `json:"previous_is_before"`
	NumFoundText     string `json:"num_found_text"`
	TotalNumFound    int    `json:"total_num_found"`
	IsApproximate    bool   `json:"is_approximate"`
}

type Price struct {
	Date  time.Time `bson:"date"`
	Price float64   `bson:"price"`
}

func extractPrice(buy map[string]interface{}) (float64, error) {
	prices, exists := buy["prices"]
	if !exists {
		return 0, fmt.Errorf("Prices key does not exist")
	}

	priceSlice, ok := prices.([]interface{})
	if !ok || len(priceSlice) == 0 {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	price, ok := priceSlice[0].(float64)
	if !ok {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	return price, nil
}

func extractAppPrice(buy map[string]interface{}) (float64, error) {
	prices, exists := buy["app_prices"]
	if !exists {
		return 0, fmt.Errorf("Prices key does not exist")
	}

	priceSlice, ok := prices.([]interface{})
	if !ok || len(priceSlice) == 0 {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	price, ok := priceSlice[0].(float64)
	if !ok {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	return price, nil
}

func processGalleryImages(productView map[string]interface{}) ([]string, error) {
	if gallery, ok := productView["images"]; ok {
		if images, ok := gallery.([]interface{}); ok {
			var processedImages []string
			for _, image := range images {
				if imageUrl, ok := image.(string); ok {
					// Manipulate the imageUrl as needed
					processedImageUrl := strings.ReplaceAll(imageUrl, "{size}", "zoom")
					processedImages = append(processedImages, processedImageUrl)
				} else {
					return nil, fmt.Errorf("Image URL is not a string")
				}
			}
			return processedImages, nil
		} else {
			return nil, fmt.Errorf("Images are not in the expected format")
		}
	} else {
		return nil, fmt.Errorf("Gallery field does not exist")
	}
}

func extractSection(data map[string]interface{}) {
	for _, value := range data {
		if obj, ok := value.(map[string]interface{}); ok {
			if products, exists := obj["products"]; exists {
				if paging, exists := products.(map[string]interface{})["paging"]; exists {
					fmt.Printf("after:%s\n", after)
					fmt.Printf("next_is_after:%s\n", paging.(map[string]interface{})["next_is_after"])
					after = paging.(map[string]interface{})["next_is_after"].(string)
				}

				if hasNext, exists := products.(map[string]interface{})["is_paged"]; exists {
					fmt.Printf("is paged:%v\n\n", hasNext)
					isPaged = hasNext.(bool)
				}

				
				if results, exists := products.(map[string]interface{})["results"]; exists {
					if resultSlice, ok := results.([]interface{}); ok {
						for _, result := range resultSlice {
							if resultObj, ok := result.(map[string]interface{}); ok {
								if productView, _ := resultObj["product_views"]; exists {
									var (
										title string
										brand string 
										images []string
										price float64
										slug string
									)

									if core, ok := productView.(map[string]interface{})["core"]; ok{
										title = core.(map[string]interface{})["title"].(string)
										slug = core.(map[string]interface{})["title"].(string)
										
										if ItemBrand, ok := core.(map[string]interface{})["brand"]; ok{
											if(ItemBrand != nil){
												brand = ItemBrand.(string)
											}
										} 
									}

									if gallery, ok := productView.(map[string]interface{})["gallery"]; ok{
										processedImages, err := processGalleryImages(gallery.(map[string]interface{}))
										
										if(err != nil){
											fmt.Println("Error:", err)
											return
										}
										images = processedImages
									}

									if buy, ok := productView.(map[string]interface{})["buybox_summary"]; ok{
										returnedPrice, _ :=  extractPrice(buy.(map[string]interface{})) 
										price = returnedPrice
									}

									saveToDatabase(title, brand, images, price, slug)
								}
							}
						}
					}
				}
			}
		}
	}
}

func initialize(){
	if err := godotenv.Load(); err != nil{
		log.Println("No .env file found")
		panic(err)
	}

	mongoUri = os.Getenv("MONGODB_URI")
	mongoDatabase = os.Getenv("MONGODB_DATABASE")
	mongoCollection = os.Getenv("COLLECTION")
	baseURL = os.Getenv("TAKEALOT_BASE_URL")

	if mongoUri == "" {
		panic("You must set your 'MONGODB_URI' enviroment variable")
	}

	if mongoDatabase == "" {
		panic("You must set your 'MONGODB_URI' enviroment variable")
	}

	if mongoCollection == "" {
		panic("You must set your 'MONGODB_URI' enviroment variable")
	}

	if baseURL == "" {
		panic("You must set your 'MONGODB_URI' enviroment variable")
	}
}

func connectMongoDB(){
	mongoClient, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(mongoUri))
	if err != nil {
		panic(err)
	}

	err = mongoClient.Ping(context.TODO(), nil)
	if err != nil {
		panic(err)
	}

	connectedToDatabase = true
	fmt.Println("Connected to MongoDB")
}

func saveToDatabase(title string, brand string,  images []string, price float64, slug string){
	link := "https://www.takealot.com"

	defer func() {
		if mongoClient != nil {
			if err := mongoClient.Disconnect(context.TODO()); err != nil {
				panic(err)
			}
		}
	}()

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(mongoUri))
	if err != nil {
		panic(err)
	}

	filter := bson.M{"title": title}
	now := time.Now()
	
	update := bson.M{
		"$push": bson.M{
			"prices": bson.M{
				"$each": []Price{
					{
						Date:  now,
						Price: price,
					},
				},
			},
		},
		"$set": bson.M{
			"title":  title,
			"images": images,
			"brand": brand,
			"lastUpdate": now,
			"source": bson.M{
				"website": link,
				"slug": slug,
				"id": nil,
			},
		},
	}

	collection := client.Database(mongoDatabase).Collection(mongoCollection)
	opts := options.Update().SetUpsert(true)
	result, err := collection.UpdateOne(context.TODO(), filter, update, opts)

	if err != nil {
		panic(err)
	}

	if result.UpsertedCount > 0 {
		fmt.Println("Document was created")
	} else {
		fmt.Println("Document was found and updated")
	}
	client.Disconnect(context.TODO());
}

func main() {
	initialize()
	
	//TODO: get list from database
	words = []string {"fridge", "couch", "milk", "bread", "coffee"}
	client := &http.Client{}

		for _, word := range words {
			page := 1

			for {
				apiURL := fmt.Sprintf("%s&qsearch=%s", baseURL, word, page)
				if after != "" {
					apiURL += fmt.Sprintf("&after=%s", after)
				}

				req, err := http.NewRequest("GET", apiURL, nil)
				if err != nil {
					fmt.Println("Error creating the request:", err)
					return
				}

				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
				req.Header.Set("Accept-Language", "en-US,en;q=0.9")
				req.Header.Set("Referer", "https://takealot.com")

				response, err := client.Do(req)
				if err != nil {
					fmt.Println("Error making the request:", err)
					return
				}
				defer response.Body.Close()

				body, err := io.ReadAll(response.Body)
				if err != nil {
					fmt.Println("Error reading the response body:", err)
					return
				}

				var apiResponse ApiResponse
				err = json.Unmarshal(body, &apiResponse)
				if err != nil {
					fmt.Println("Error decoding JSON:", err)
					return
				}

				var jsonData map[string]interface{}
				err = json.Unmarshal(body, &jsonData)
				if err != nil {
					fmt.Println("Error decoding JSON:", err)
					return
				}

				extractSection(jsonData)
				
				if !isPaged || after == "" {
				 	break
				}

				page++
				time.Sleep(5 * time.Second)
			}
		}

}