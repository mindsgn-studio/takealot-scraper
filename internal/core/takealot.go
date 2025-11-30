package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

func OpenPageTakealot(link string) (Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	browserCtx, _ := chromedp.NewContext(ctx)

	var html string
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(link),
		chromedp.WaitVisible(`span.currency.plus`, chromedp.ByQuery),
		chromedp.OuterHTML("body", &html),
	); err != nil {
		return Item{Link: link, Source_Name: "takealot"}, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Item{}, err
	}

	priceText := doc.Find("span.currency.plus").First().Text()
	price := extractTakealotPrice(strings.TrimSpace(priceText))

	title := strings.TrimSpace(doc.Find("h1").First().Text())

	brand := strings.TrimSpace(doc.Find("span.brand-link").First().Text())

	ratingString := doc.Find("span.score").First().Text()
	rating, _ := strconv.ParseFloat(ratingString, 64)
	var images []string
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if ref, _ := s.Attr("data-ref"); strings.Contains(ref, "main-gallery-photo") {
			if src, exists := s.Attr("src"); exists {
				images = append(images, src)
			}
		}
	})

	mainImage := ""
	if len(images) > 0 {
		mainImage = images[0]
	}

	id := ExtractTakealotID(link)
	if id == "" {
		return Item{}, fmt.Errorf("could not extract product ID from url: %s", link)
	}
	var isAvailable = false
	inStock := doc.Find("span.stock-availability-status").First().Text()
	if inStock == "In stock" {
		isAvailable = true
	}

	item := Item{
		ID:             id,
		Link:           link,
		Title:          title,
		Brand:          brand,
		Images:         images,
		Image:          mainImage,
		Overall_Rating: rating,
		Source_Name:    "takealot",
		Current_Price:  price,
		In_Stock:       isAvailable,
	}

	return item, nil
}
