package main

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
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gocolly/colly"
	"github.com/mindsgn-studio/takealot-scraper/internal/config"
	"github.com/mindsgn-studio/takealot-scraper/internal/model"
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

var totalPages int

type Scraper struct {
	cfg         model.Config
	mongoClient *mongo.Client
	db          *mongo.Database
	httpClient  *http.Client
	logger      *log.Logger
	itemsColl   *mongo.Collection
	pricesColl  *mongo.Collection
}

type JsonObject map[string]interface{}

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

func (s *Scraper) Items(ctx context.Context) ([]string, error) {
	s.logger.Printf("Started watching items")
	db := s.mongoClient.Database("snapprice")
	coll := db.Collection("items")
	var brands []string
	items := 0

	filter := bson.M{
		"brand": bson.M{"$exists": true, "$ne": ""},
	}

	cursor, err := coll.Find(ctx, filter, options.Find().SetProjection(bson.M{"brand": 1}))
	if err != nil {
		return nil, fmt.Errorf("find items with null brand: %w", err)
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

func (s *Scraper) SaveItemData(parentCtx context.Context, title string, images []string, link string, id string, brand string) (primitive.ObjectID, error) {
	ctx, cancel := context.WithTimeout(parentCtx, DefaultDBOpTimeout)
	defer cancel()

	filter := bson.M{
		"sources.id":     id,
		"sources.source": "amazon",
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

	if oid, ok := updatedDoc["_id"].(primitive.ObjectID); ok {
		return oid, nil
	}

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

	doc := model.Price{
		ItemID:   itemID,
		Date:     time.Now().UTC(),
		Currency: "zar",
		Price:    priceVal,
	}

	_, err := s.pricesColl.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("insert price: %w", err)
	}

	return nil
}

func extractPrice(text string) (float64, error) {
	re := regexp.MustCompile(`R[ \xA0]?([\d \xA0]+,\d{2})`)
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return 0, fmt.Errorf("No price found")
	}
	clean := strings.ReplaceAll(match[1], " ", "")
	clean = strings.ReplaceAll(clean, "\u00A0", "")
	clean = strings.Replace(clean, ",", ".", 1)
	price, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, fmt.Errorf("Error parsing price")
	}
	return price, nil
}

func (s *Scraper) ScrapeBrand(ctx context.Context, brand string) error {
	page := 1
	var title string
	var itemLink string
	var itemID string = ""
	var images []string
	var price float64

	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("div.s-result-list.s-search-results.sg-row", func(h *colly.HTMLElement) {
		h.ForEach("div.sg-col-4-of-24.sg-col-4-of-12.s-result-item.s-asin.sg-col-4-of-16.sg-col.s-widget-spacing-small.sg-col-4-of-20", func(_ int, cardElement *colly.HTMLElement) {

			itemID = cardElement.Attr("data-asin")

			title = cardElement.ChildText("h2.a-size-base-plus.a-color-base.a-text-normal")

			cardElement.ForEach("a.a-link-normal.s-no-outline", func(_ int, h *colly.HTMLElement) {
				itemLink = "https://www.amazon.co.za" + h.Attr("href")
			})

			text := cardElement.ChildText("span.a-offscreen")
			price, _ = extractPrice(text)

			cardElement.ForEach("img.s-image", func(_ int, h *colly.HTMLElement) {
				images = append(images, h.Attr("src"))
			})

			if itemID != "" {
				id, _ := s.SaveItemData(ctx, title, images, itemLink, itemID, "")
				s.logger.Print("saved Item", id)
				s.SavePriceIfStale(ctx, id, price)
			}
		})

		h.ForEach("span.s-pagination-item.s-pagination-disabled", func(_ int, h *colly.HTMLElement) {
			if h.Text != "Previous" {
				number, err := strconv.Atoi(h.Text)
				if err != nil {
					return
				}

				if page == 1 {
					totalPages = number
				}
			}
		})
	})

	for {
		var link = fmt.Sprintf("https://www.amazon.co.za/s?k=%s&page=%d", url.QueryEscape(brand), page)

		collyClient.Visit(link)
		collyClient.Wait()

		if page >= totalPages {
			break
		}

		s.logger.Print(brand, " page: ", page)
		page++
	}

	return nil
}

func NewScraper(cfg model.Config, logger *log.Logger) (*Scraper, error) {
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

	if err := s.ensureIndexes(context.Background()); err != nil {
		logger.Printf("warning: could not ensure indexes: %v", err)
	}
	return s, nil
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

func main() {
	logger := log.New(os.Stdout, "[Amazon] ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := config.LoadConfig()
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
