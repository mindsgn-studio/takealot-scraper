package category

import (
	"math/rand"
)

var categoryList = []string{
	"automotive",
	"diy",
	"baby",
	"toddler",
	"beauty",
	"books",
	"courses",
	"camping",
	"outdoor",
	"clothing",
	"shoes",
	"accessories",
	"electronics",
	"gaming",
	"media",
	"garden",
	"pool",
	"patio",
	"groceries",
	"household",
	"health",
	"personal care",
	"home",
	"appliences",
	"liquor",
	"office",
	"stationary",
	"pets",
	"sport",
	"training",
	"toys",
}

func GetRandomCategory() string {
	randomIndex := rand.Intn(len(categoryList))
	return categoryList[randomIndex]
}
