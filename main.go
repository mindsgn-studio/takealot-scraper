package main

import (
	"flag"
	"fmt"

	"github.com/mindsgn-studio/takealot-scraper/scraper"
)

func main() {
	var input string

	flag.StringVar(&input, "command", "", "Path to the input file")
	flag.Parse()

	if input == "" {
		fmt.Println("Error: Missing input file. Please use the --command flag.")
		return
	}

	if input == "scrape" {
		scraper.GetBrand()
	}

	if input == "watch" {
		return
	}
}
