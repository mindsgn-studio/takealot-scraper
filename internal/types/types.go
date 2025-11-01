package types

import (
	"log"
	"net/http"

	"github.com/mindsgn-studio/takealot-scraper/internal/model"
	"go.mongodb.org/mongo-driver/mongo"
)

type Scraper struct {
	cfg         model.Config
	mongoClient *mongo.Client
	db          *mongo.Database
	httpClient  *http.Client
	logger      *log.Logger
	itemsColl   *mongo.Collection
	pricesColl  *mongo.Collection
}
