package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
	"github.com/mindsgn-studio/takealot-scraper/internal/model"
)

const (
	DefaultDBName     = "snapprice"
	DefaultItemsColl  = "items"
	DefaultPricesColl = "prices"
)

func LoadConfig() (model.Config, error) {
	// load .env if present but don't error if not present
	_ = godotenv.Load()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		return model.Config{}, errors.New("MONGODB_URI not set")
	}

	db := os.Getenv("MONGO_DB_NAME")
	if db == "" {
		db = DefaultDBName
	}

	brandFile := os.Getenv("BRAND_FILE")
	if brandFile == "" {
		brandFile = "brand.txt"
	}

	ua := os.Getenv("USER_AGENT")
	if ua == "" {
		ua = "snapprice-scraper/1.0 (+https://example.com)"
	}

	return model.Config{
		MongoURI:   mongoURI,
		DBName:     db,
		ItemsColl:  DefaultItemsColl,
		PricesColl: DefaultPricesColl,
		BrandFile:  brandFile,
		UserAgent:  ua,
	}, nil
}
