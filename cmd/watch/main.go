package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	DefaultDBName        = "snapprice"
	DefaultItemsColl     = "items"
	DefaultPricesColl    = "prices"
	DefaultBrandColl     = "brand"
	DefaultHTTPTimeout   = 20 * time.Second
	DefaultDBOpTimeout   = 10 * time.Second
	PriceDedupWindow     = 2 * time.Hour
	HTTPMaxRetries       = 3
	HTTPRetryBaseBackoff = 500 * time.Millisecond
)

type Config struct {
	MongoURI   string
	DBName     string
	ItemsColl  string
	PricesColl string
	brandColl  string
	BrandFile  string
	UserAgent  string
}

type Price struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	ItemID   primitive.ObjectID `bson:"itemID"`
	Date     time.Time          `bson:"date"`
	Currency string             `bson:"currency"`
	Price    float64            `bson:"price"`
}

type Scraper struct {
	cfg         Config
	mongoClient *mongo.Client
	db          *mongo.Database
	httpClient  *http.Client
	logger      *log.Logger
	itemsColl   *mongo.Collection
	pricesColl  *mongo.Collection
	brandColl   *mongo.Collection
}

type JsonObject map[string]interface{}

func loadConfig() (Config, error) {
	_ = godotenv.Load()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		return Config{}, errors.New("MONGODB_URI not set")
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
		ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}

	return Config{
		MongoURI:   mongoURI,
		DBName:     db,
		ItemsColl:  DefaultItemsColl,
		PricesColl: DefaultPricesColl,
		brandColl:  DefaultBrandColl,
		BrandFile:  brandFile,
		UserAgent:  ua,
	}, nil
}

func NewScraper(cfg Config, logger *log.Logger) (*Scraper, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(cfg.MongoURI)
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	db := client.Database(cfg.DBName)
	s := &Scraper{
		cfg:         cfg,
		mongoClient: client,
		db:          db,
		httpClient: &http.Client{
			Timeout: DefaultHTTPTimeout,
		},
		logger:     logger,
		itemsColl:  db.Collection(cfg.ItemsColl),
		pricesColl: db.Collection(cfg.PricesColl),
		brandColl:  db.Collection(cfg.brandColl),
	}

	if err := s.ensureIndexes(context.Background()); err != nil {
		logger.Printf("warning: could not ensure indexes: %v", err)
	}
	return s, nil
}

func (s *Scraper) Close(ctx context.Context) error {
	return s.mongoClient.Disconnect(ctx)
}

func (s *Scraper) ensureIndexes(ctx context.Context) error {
	_, err := s.itemsColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "sources.id", Value: 1}, {Key: "sources.source", Value: 1}},
		Options: options.Index().SetUnique(false),
	})
	if err != nil {
		return err
	}
	_, err = s.pricesColl.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "itemID", Value: 1}, {Key: "date", Value: -1}},
	})
	return err
}

func (s *Scraper) SaveBrand(parentCtx context.Context, brand string) {
	ctx, cancel := context.WithTimeout(parentCtx, DefaultDBOpTimeout)
	defer cancel()

	filter := bson.M{"brand": brand}
	update := bson.M{
		"$setOnInsert": bson.M{
			"brand": brand,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := s.brandColl.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		s.logger.Printf("upsert brand: %w", err)
	}

	s.logger.Printf("upsert brand: ")
}

func (s *Scraper) FixAll(parentCtx context.Context) {
	s.logger.Printf("Started fixing items")
	db := s.mongoClient.Database("snapprice")
	coll := db.Collection("prices")
	filter := bson.M{}
	update := bson.M{"$rename": bson.M{"item_id": "itemID"}}

	result, err := coll.UpdateMany(parentCtx, filter, update)
	if err != nil {
		log.Fatalf("Failed to rename field: %v", err)
	}

	fmt.Printf("Modified %d documents\n", result.ModifiedCount)
}

func (s *Scraper) Run(ctx context.Context) error {
	s.FixAll(ctx)
	return nil
}

func (s *Scraper) Items(ctx context.Context) error {
	s.logger.Printf("Started watching items")
	db := s.mongoClient.Database("snapprice")
	coll := db.Collection("items")
	var brands []string

	filter := bson.M{}

	cursor, err := coll.Find(ctx, filter, options.Find().SetProjection(bson.M{"brand": 1}))
	if err != nil {
		return fmt.Errorf("find items with null brand: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			Brand string `bson:"brand"`
		}

		if err := cursor.Decode(&doc); err != nil {
			s.logger.Printf("decode error: %v", err)
			continue
		}

		if doc.Brand != "" {
			s.logger.Print(doc.Brand)
			s.SaveBrand(ctx, doc.Brand)
		}
	}

	if err := cursor.Err(); err != nil {
		return fmt.Errorf("cursor error: %w", err)
	}

	s.logger.Printf("stopped watching items: %d", len(brands))
	return nil
}

func main() {
	logger := log.New(os.Stdout, "[scraper] ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := loadConfig()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	scraper, err := NewScraper(cfg, logger)
	if err != nil {
		logger.Fatalf("new scraper: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := scraper.Close(ctx); err != nil {
			logger.Printf("error disconnecting mongo: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := scraper.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Printf("run error: %v", err)
	}
	logger.Print("scraper finished")
}
