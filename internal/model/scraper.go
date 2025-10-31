package model

import (
	"log"
	"net/http"

	"go.mongodb.org/mongo-driver/mongo"
)

type Scraper struct {
	cfg         Config
	mongoClient *mongo.Client
	db          *mongo.Database
	httpClient  *http.Client
	logger      *log.Logger
	itemsColl   *mongo.Collection
	pricesColl  *mongo.Collection
}
