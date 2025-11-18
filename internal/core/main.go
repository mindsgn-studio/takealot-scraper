package core

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gocolly/colly"
)

func ExtractText(text string) float64 {
	re := regexp.MustCompile(`R[\s\p{Zs}]*([\d\s\p{Zs}]+,\d{2})`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		price := matches[1]
		price = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\u00A0' {
				return -1
			}
			return r
		}, price)
		fmtPrice, err := strconv.ParseFloat(price, 64)
		if err != nil {
			return 0
		}

		return fmtPrice
	}
	return 0
}

func OpenAmazonPage(link string) float64 {
	var currentPrice float64
	collyClient := colly.NewCollector()
	collyClient.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	collyClient.OnHTML("body", func(body *colly.HTMLElement) {
		body.ForEach("div.a-section.a-spacing-none.aok-align-center.aok-relative", func(index int, element *colly.HTMLElement) {
			currentPrice = ExtractText(element.Text)
			return
		})
	})

	collyClient.Visit(link)
	collyClient.Wait()
	return currentPrice
}
