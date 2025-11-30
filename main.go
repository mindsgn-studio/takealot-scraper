package main

import (
	"fmt"

	"github.com/mindsgn-studio/takealot-scraper/internal/core"
)

func main() {
	//	getTakealot("https://www.takealot.com/midea-6kg-front-loader-1000rpm-titanium/PLID93155744")
	//	getWeBuyCars("https://www.webuycars.co.za/buy-a-car/Citroen/C3/3C3D13342")
	getCheckers("https://www.checkers.co.za/product/snomaster-white-counter-top-ice-maker-12kg-10893171EA")
}

func getTakealot(link string) {
	data, err := core.OpenPageTakealot(link)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("title: ", data.Title)
	fmt.Println("Current Price: ", data.Current_Price)
	fmt.Println("Rating: ", data.Overall_Rating)
	fmt.Println("Image: ", data.Image)
	fmt.Println("In Stock: ", data.In_Stock)
	fmt.Println("\n")
}

func getWeBuyCars(link string) {
	data, err := core.OpenWeBuyCars(link)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("title: ", data.Title)
	fmt.Println("Current Price: ", data.Current_Price)
	fmt.Println("Rating: ", data.Overall_Rating)
	fmt.Println("Image: ", data.Image)
	fmt.Println("In Stock: ", data.In_Stock)
	fmt.Println("\n")
}

func getCheckers(link string) {
	data, err := core.OpenCheckers(link)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("title: ", data.Title)
	fmt.Println("Current Price: ", data.Current_Price)
	fmt.Println("Rating: ", data.Overall_Rating)
	fmt.Println("Image: ", data.Image)
	fmt.Println("In Stock: ", data.In_Stock)
	fmt.Println("\n")
}
