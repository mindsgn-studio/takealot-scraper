package takealot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	BrandFile  string
	UserAgent  string
}

type Price struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	ItemID   primitive.ObjectID `bson:"item_id"`
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
}

type JsonObject map[string]interface{}

func loadConfig() (Config, error) {
	// load .env if present but don't error if not present
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
		ua = "snapprice-scraper/1.0 (+https://example.com)"
	}

	return Config{
		MongoURI:   mongoURI,
		DBName:     db,
		ItemsColl:  DefaultItemsColl,
		PricesColl: DefaultPricesColl,
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
	}

	// ensure indexes are present (best-effort)
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
		Keys: bson.D{{Key: "item_id", Value: 1}, {Key: "date", Value: -1}},
	})
	return err
}

func (s *Scraper) LoadBrands(brands []string) ([]string, error) {

	data, err := os.ReadFile(s.cfg.BrandFile)
	if err != nil {
		return nil, fmt.Errorf("read brand file: %w", err)
	}

	raw := strings.Split(string(data), ",")
	for _, r := range raw {
		t := strings.TrimSpace(r)
		if t != "" {
			brands = append(brands, t)
		}
	}

	if len(brands) == 0 {
		return nil, errors.New("no brands found")
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(brands), func(i, j int) { brands[i], brands[j] = brands[j], brands[i] })

	return brands, nil
}

func (s *Scraper) Run(ctx context.Context) error {
	brandList, err := s.Items(ctx)
	if err != nil {
		return fmt.Errorf("load items: %w", err)
	}

	brands, err := s.LoadBrands(brandList)
	if err != nil {
		return err
	}

	rand.Seed(time.Now().UnixNano())

	for _, brand := range brands {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		s.logger.Printf("START brand=%s", brand)
		if err := s.ScrapeBrand(ctx, brand); err != nil {
			s.logger.Printf("error scraping brand=%s: %v", brand, err)
		}

		time.Sleep(time.Second*1 + time.Duration(rand.Intn(2000))*time.Millisecond/1000)
	}
	return nil
}

func (s *Scraper) ScrapeBrand(ctx context.Context, brand string) error {
	after := ""
	page := 1
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		s.logger.Printf("fetching page=%d brand=%s after=%q", page, brand, after)
		respData, nextAfter, err := s.FetchPage(ctx, brand, after)
		if err != nil {
			return fmt.Errorf("fetch page: %w", err)
		}

		if err := s.ParseAndPersist(ctx, respData); err != nil {
			s.logger.Printf("parse error brand=%s page=%d: %v", brand, page, err)
		}

		if nextAfter == "" {
			break
		}
		after = nextAfter
		page++
		time.Sleep(time.Millisecond*300 + time.Duration(rand.Intn(700))*time.Millisecond)
	}
	s.logger.Printf("finished brand=%s", brand)
	return nil
}

func (s *Scraper) FetchPage(parentCtx context.Context, item string, after string) (JsonObject, string, error) {
	escaped := url.QueryEscape(item)
	apiURL := fmt.Sprintf("https://api.takealot.com/rest/v-1-14-0/searches/products?newsearch=true&qsearch=%s&track=1&userinit=true&searchbox=true", escaped)
	if after != "" {
		apiURL += "&after=" + url.QueryEscape(after)
	}

	var lastErr error
	for attempt := 0; attempt < HTTPMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := HTTPRetryBaseBackoff * time.Duration(1<<(attempt-1))
			jitter := time.Duration(rand.Intn(300)) * time.Millisecond
			time.Sleep(backoff + jitter)
		}

		req, err := http.NewRequestWithContext(parentCtx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("User-Agent", s.cfg.UserAgent)

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			s.logger.Printf("http request attempt=%d error=%v", attempt+1, err)
			continue
		}

		// ensure body closed
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			s.logger.Printf("non-200 status attempt=%d code=%d", attempt+1, resp.StatusCode)
			continue
		}

		var data JsonObject
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&data); err != nil {
			return nil, "", fmt.Errorf("decode json: %w", err)
		}

		// extract paging.next_is_after if present
		nextAfter := ""
		if sections, ok := data["sections"].(map[string]interface{}); ok {
			if products, ok := sections["products"].(map[string]interface{}); ok {
				if paging, ok := products["paging"].(map[string]interface{}); ok {
					if n, ok := paging["next_is_after"].(string); ok {
						nextAfter = n
					}
				}
			}
		}

		return data, nextAfter, nil
	}

	return nil, "", fmt.Errorf("http fetch failed: %w", lastErr)
}

func (s *Scraper) ParseAndPersist(ctx context.Context, data JsonObject) error {
	sections, ok := data["sections"].(map[string]interface{})
	if !ok {
		return errors.New("sections missing")
	}
	products, ok := sections["products"].(map[string]interface{})
	if !ok {
		return errors.New("products missing")
	}
	results, ok := products["results"].([]interface{})
	if !ok {
		return errors.New("results missing or wrong type")
	}

	for _, r := range results {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resultMap, ok := r.(map[string]interface{})
		if !ok {
			s.logger.Print("skipping invalid result format")
			continue
		}
		pv, ok := resultMap["product_views"].(map[string]interface{})
		if !ok {
			s.logger.Print("missing product_views; skipping")
			continue
		}
		if err := s.extractItemData(ctx, pv); err != nil {
			s.logger.Printf("extractItemData error: %v", err)
			// continue to next result
		}
	}
	return nil
}

func toStringSlice(in interface{}) ([]string, error) {
	switch v := in.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, it := range v {
			if s, ok := it.(string); ok {
				imageURL := strings.ReplaceAll(s, "{size}", "zoom")
				out = append(out, imageURL)
			}
		}

		return out, nil
	case string:
		return []string{v}, nil
	default:
		return []string{}, fmt.Errorf("unexpected image type %T", in)
	}
}

func (s *Scraper) getProductIDFromProducts(products interface{}) (string, error) {
	switch v := products.(type) {
	case []interface{}:
		for _, p := range v {
			if pm, ok := p.(map[string]interface{}); ok {
				if id, ok := pm["id"].(string); ok {
					return id, nil
				}
			}
		}
	case map[string]interface{}:
		if id, ok := v["id"].(string); ok {
			return id, nil
		}
	}
	return "", errors.New("product id not found")
}

func (s *Scraper) extractItemData(parentCtx context.Context, item map[string]interface{}) error {
	core, _ := item["core"].(map[string]interface{})
	gallery, _ := item["gallery"].(map[string]interface{})
	buySummary, _ := item["buybox_summary"].(map[string]interface{})
	enhanced, _ := item["enhanced_ecommerce_click"].(map[string]interface{})

	if core == nil || gallery == nil || buySummary == nil || enhanced == nil {
		return nil
	}

	imagesField, ok := gallery["images"]
	if !ok {
		return nil
	}

	images, err := toStringSlice(imagesField)
	if err != nil {
		s.logger.Printf("images parse warning: %v", err)
	}

	title, _ := core["title"].(string)
	brand, _ := core["brand"].(string)
	slug, _ := core["slug"].(string)

	if title == "" || brand == "" || slug == "" {
		return nil
	}

	ecommerce, ok := enhanced["ecommerce"].(map[string]interface{})
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
	id, err := s.getProductIDFromProducts(products)
	if err != nil {
		return nil
	}

	plid := strings.ReplaceAll(id, "PLID", "")
	link := fmt.Sprintf("https://www.takealot.com/%s/%s", slug, id)

	pricesField, ok := buySummary["prices"]
	if !ok {
		return nil
	}

	price, err := extractPrice(pricesField)
	if err != nil {
		return nil
	}

	// Save item (upsert) and get item _id
	itemID, err := s.SaveItemData(parentCtx, title, images, link, plid, brand)
	if err != nil {
		s.logger.Printf("save item failed: %v", err)
		return nil // move on — don't abort
	}

	// Save price (dedupe within time window)
	if err := s.SavePriceIfStale(parentCtx, itemID, price); err != nil {
		s.logger.Printf("save price failed for item %s: %v", itemID.Hex(), err)
		// non-fatal
	}
	return nil
}

func extractPrice(prices interface{}) (float64, error) {
	switch v := prices.(type) {
	case []interface{}:
		if len(v) == 0 {
			return 0, errors.New("empty prices array")
		}

		switch n := v[0].(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		case int32:
			return float64(n), nil
		case int64:
			return float64(n), nil
		case string:
			f, err := strconvParseFloat(n)
			if err != nil {
				return 0, fmt.Errorf("price parse error: %w", err)
			}
			return f, nil
		default:
			return 0, fmt.Errorf("unsupported price type %T", n)
		}
	case float64:
		return v, nil
	case string:
		return strconvParseFloat(v)
	default:
		return 0, fmt.Errorf("unsupported prices field type %T", v)
	}
}

func strconvParseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty price string")
	}

	s = strings.ReplaceAll(s, ",", "")
	return strconv.ParseFloat(s, 64)
}

func (s *Scraper) SaveItemData(parentCtx context.Context, title string, images []string, link string, id string, brand string) (primitive.ObjectID, error) {
	ctx, cancel := context.WithTimeout(parentCtx, DefaultDBOpTimeout)
	defer cancel()

	filter := bson.M{
		"sources.id":     id,
		"sources.source": "takealot",
	}
	update := bson.M{
		"$set": bson.M{
			"title":   title,
			"images":  images,
			"link":    link,
			"brand":   brand,
			"updated": time.Now().UTC(),
		},
		"$setOnInsert": bson.M{
			"created": time.Now().UTC(),
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var updatedDoc bson.M
	err := s.itemsColl.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedDoc)
	if err != nil {
		// For some drivers, decode into nil on upsert can error; fallback to FindOne
		if err == mongo.ErrNoDocuments {
			var doc bson.M
			if err2 := s.itemsColl.FindOne(ctx, filter).Decode(&doc); err2 == nil {
				updatedDoc = doc
			} else {
				return primitive.NilObjectID, fmt.Errorf("find after upsert failed: %w / %v", err, err2)
			}
		} else {
			return primitive.NilObjectID, fmt.Errorf("findoneandupdate: %w", err)
		}
	}

	// Ensure we return ObjectID
	if oid, ok := updatedDoc["_id"].(primitive.ObjectID); ok {
		return oid, nil
	}
	// If the driver returned _id as primitive.ObjectID but wrapped differently, try conversion
	if idVal, ok := updatedDoc["_id"].(string); ok {
		oid, err := primitive.ObjectIDFromHex(idVal)
		if err == nil {
			return oid, nil
		}
	}
	return primitive.NilObjectID, errors.New("could not resolve item _id after upsert")
}

func (s *Scraper) SavePriceIfStale(parentCtx context.Context, itemID primitive.ObjectID, priceVal float64) error {
	ctx, cancel := context.WithTimeout(parentCtx, DefaultDBOpTimeout)
	defer cancel()

	threshold := time.Now().Add(-PriceDedupWindow)
	filter := bson.M{
		"item_id": itemID,
		"date":    bson.M{"$gt": threshold},
	}

	count, err := s.pricesColl.CountDocuments(ctx, filter)
	if err != nil {
		return fmt.Errorf("count recent prices: %w", err)
	}
	if count > 0 {
		return nil
	}

	doc := Price{
		ItemID:   itemID,
		Date:     time.Now().UTC(),
		Currency: "zar",
		Price:    priceVal,
	}
	_, err = s.pricesColl.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("insert price: %w", err)
	}
	return nil
}

func uniqueStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, v := range input {
		if _, exists := seen[v]; !exists {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func (s *Scraper) Items(ctx context.Context) ([]string, error) {
	s.logger.Printf("Started watching items")
	db := s.mongoClient.Database("snapprice")
	coll := db.Collection("items")
	var brands []string
	items := 0

	filter := bson.M{}

	cursor, err := coll.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("find items with null brand: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			Title string `bson:"title"`
			Brand string `bson:"brand"`
		}
		if err := cursor.Decode(&doc); err != nil {
			s.logger.Printf("decode error: %v", err)
			continue
		}

		if doc.Title != "" {
			brands = append(brands, doc.Brand)
			items++
		}
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	uniqueBrands := uniqueStrings(brands)

	s.logger.Printf("stopped watching items: %d", len(uniqueBrands))
	return uniqueBrands, nil
}

func Start() {
	logger := log.New(os.Stdout, "[Takealot] ", log.LstdFlags|log.Lmsgprefix)

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
