package core

import (
	"context"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

func OpenWeBuyCars(link string) (Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	browserCtx, _ := chromedp.NewContext(ctx)

	var html string
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(link),
		chromedp.WaitVisible(`div.description.d-flex.flex-row`, chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		return Item{Link: link, Source_Name: "shoprite"}, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Item{}, err
	}

	title := strings.TrimSpace(doc.Find("div.description.d-flex.flex-row").First().Text())

	item := Item{
		Title: title,
	}

	return item, nil
}
