package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mindsgn-studio/takealot-scraper/internal/core"
)

func main() {
	logger := log.New(os.Stdout, "[tracker] ", log.LstdFlags|log.Lmsgprefix)

	newItem, err := core.OpenPageTakealot("https://www.takealot.com/midea-6kg-front-loader-1000rpm-titanium/PLID93155744")
	if err != nil {
		fmt.Println(err)
	} else {
		log.Println("Starting MongoDB to PostgreSQL migration...")
		mongoClient, err := core.ConnectMongo()
		if err != nil {
			logger.Fatal("Failed to connect to MongoDB:", err)
		}
		defer mongoClient.Disconnect(context.Background())

		newEntry, err := core.SaveItemData(mongoClient, newItem.Title, newItem.Images, newItem.Link, newItem.ID, newItem.Brand)
		if err != nil {
			logger.Fatal("Failed to save", err)
		}
		fmt.Println(newEntry)
	}

	/*
		client, err := ably.NewRealtime(
			ably.WithKey("2CIRYw.fvWv8A:Dg4mmFzMik-V7K8QMXCxY6c27b8VXBI9yqcV08qQn-E"),
			ably.WithClientID("local-server"),
		)
		if err != nil {
			log.Fatal(err)
		}
		defer client.Close()

		connStateChan := make(chan ably.ConnectionStateChange, 1)
		client.Connection.On(ably.ConnectionEventConnected, func(change ably.ConnectionStateChange) {
			connStateChan <- change
		})
		select {
		case <-connStateChan:
			fmt.Println("Made my first connection!")
		case <-context.Background().Done():
			log.Fatal("Context cancelled before connection established")
		}

		channel := client.Channels.Get("items")

		unsubscribe, err := channel.SubscribeAll(context.Background(), func(msg *ably.Message) {
			switch v := msg.Data.(type) {
			case string:
				channelName := "private:" + msg.ClientID
				privateChanel := client.Channels.Get(channelName)
				items, err := core.AssessItem(pgDB, v)
				if err != nil {
					fmt.Println(err)
				}

				var currentPrice float64

				if len(items) == 0 {
					data, err := core.OpenPageTakealot(v)
					if err != nil {
						fmt.Println(err)
					}

					fmt.Println(data)
				}

				item := core.Item{
					UUID:          "",
					Link:          "",
					Image:         "",
					Title:         "",
					Source_Name:   "",
					Current_Price: currentPrice,
				}

				err = privateChanel.Publish(context.Background(),
					"example",
					item,
				)
				if err != nil {
					log.Fatal(err)
				}
			default:
				log.Printf("unsupported msg.Data type: %T", msg.Data)
			}
		})

		if err != nil {
			log.Fatal(err)
		}
		defer unsubscribe()

		select {}
	*/
}
