package core

import (
	"context"
	"fmt"
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
		chromedp.WaitVisible(`h1`, chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return Item{Link: link, Source_Name: "takealot"}, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Item{}, err
	}

	priceText := doc.Find("span.currency").First().Text()
	price := extractTakealotPrice(strings.TrimSpace(priceText))

	title := strings.TrimSpace(doc.Find("h1").First().Text())

	brand := strings.TrimSpace(doc.Find("span.brand-link").First().Text())

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

	inStock := doc.Find("span.stock-availability-status").First().Text()
	isInStock := strings.Contains(strings.ToLower(inStock), "in stock")

	item := Item{
		ID:            id,
		Link:          link,
		Title:         title,
		Brand:         brand,
		Images:        images,
		Image:         mainImage,
		Source_Name:   "takealot",
		Current_Price: price,
		In_Stock:      isInStock,
	}

	return item, nil
}
