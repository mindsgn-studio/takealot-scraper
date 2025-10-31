package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Item struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Title     string             `bson:"title"`
	Images    []string           `bson:"images"`
	Link      string             `bson:"link"`
	Brand     string             `bson:"brand"`
	Source    string             `bson:"source"`
	UpdatedAt time.Time          `bson:"updated_at"`
}
