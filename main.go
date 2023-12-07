package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

type Price struct {
	Date  time.Time `bson:"date"`
	Price float64   `bson:"price"`
}

func extractPrice(prices []interface{}) (float64, error) {
	if len(prices) == 0 {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	price, ok := prices[0].(float64)
	if !ok {
		return 0, fmt.Errorf("Prices are not in the expected format")
	}

	return price, nil
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
}

func update(title string, price float64){
	filter := bson.M{"title": title}
	now := time.Now()
	update := bson.M{
		"$push": bson.M{
			"prices": Price{
				Date:  now,
				Price: price,
			},
		},
	}

	clientOptions := options.Client().ApplyURI(mongoUri)
	client, err := mongo.Connect(context.TODO(), clientOptions)
	collection := client.Database(mongoDatabase).Collection(mongoCollection)
	opts := options.Update().SetUpsert(true)
	result, err := collection.UpdateOne(context.TODO(), filter, update, opts)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Matched %v document and modified %v document(s)\n", result.MatchedCount, result.ModifiedCount)

}

func query(url string){
	client := &http.Client{}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating the request:", err)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://www.example.com")

	response, err := client.Do(req)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return
	}
	
	var jsonData map[string]interface{}
	err = json.Unmarshal(body, &jsonData)
	if err != nil {
	fmt.Println("Error decoding JSON:", err)
		return
	}

	title := jsonData["title"].(string)
	buyBox := jsonData["buybox"]

	if prices, ok  := buyBox.(map[string]interface{})["prices"] ;ok{
		price, _ := extractPrice(prices.([]interface{}))
		update(title, price)
	}
}

func main() {
	initialize()

	clientOptions := options.Client().ApplyURI(mongoUri)

	client, err := mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(context.TODO(), nil)
	if err != nil {
		log.Fatal(err)
	}

	collection := client.Database(mongoDatabase).Collection(mongoCollection)

	pipeline := mongo.Pipeline{
		{{"$sample", bson.D{{"size", 100}}}},
	}

	cursor, err := collection.Aggregate(context.TODO(), pipeline)
	if err != nil {
		log.Fatal(err)
	}

	defer cursor.Close(context.TODO())
	for cursor.Next(context.TODO()) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			log.Fatal(err)
		}

		if sources, ok := document["sources"].(primitive.M); ok {
			query(sources["api"].(string))
		}
	}

	if err := cursor.Err(); err != nil {
		log.Fatal(err)
	}

	time.Sleep(60 *time.Second)
	main()
}	