package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type JsonObject map[string]interface{}

var collection *mongo.Collection
var ctx = context.TODO()

func connectDatabase() {
	mongoURI := os.Getenv("MONGODB_URI")
	_, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))

	if err != nil {
		panic(err)
	}
}

func getBrand() {
	getItems()
}

func getItems() {
	url := "https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=sony&track=1&userinit=true&searchbox=true"

	client := &http.Client{}

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	response, err := client.Do(request)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		fmt.Printf("Error: API return status code %d\n", response.StatusCode)
		return
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	var data JsonObject
	err = json.Unmarshal(body, &data)
	if err != nil {
		fmt.Println(err)
		return
	}

	if sections, ok := data["sections"].(map[string]interface{}); ok {
		if products, ok := sections["products"].(map[string]interface{}); ok {
			if result, ok := products["results"].(map[string]interface{}); ok {
				fmt.Println(result)
			}

			/*
				if paging, ok := products["paging"].(map[string]interface{}); ok {
					if nextIsAfer, ok := paging["next_is_after"].(map[string]interface{}); ok {
						fmt.Println(nextIsAfer)
					}
				}
			*/
		}
	}
}

func main() {
	connectDatabase()
	getBrand()
}
