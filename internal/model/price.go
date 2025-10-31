package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Price struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	ItemID   primitive.ObjectID `bson:"item_id"`
	Date     time.Time          `bson:"date"`
	Currency string             `bson:"currency"`
	Price    float64            `bson:"price"`
}
