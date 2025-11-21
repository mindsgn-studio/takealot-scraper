package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ably/ably-go/ably"
	"github.com/joho/godotenv"
	"github.com/mindsgn-studio/takealot-scraper/internal/core"
)

func main() {
	logger := log.New(os.Stdout, "[tracker] ", log.LstdFlags|log.Lmsgprefix)
	_ = godotenv.Load()

	albyKey := os.Getenv("ABLY_KEY")
	if albyKey == "" {
		logger.Fatal("Failed to get Alby Key:")
	}

	client, err := ably.NewRealtime(
		ably.WithKey(albyKey),
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

			newItem, err := core.OpenPageTakealot(v)
			if err != nil {
				fmt.Println(err)
				return
			} else {
				fmt.Println("saving data")
				newEntry, err := core.SaveItemData(newItem.Title, newItem.Images, newItem.Link, newItem.ID, newItem.Brand)
				if err != nil {
					logger.Fatal("Failed to save", err)
				}
				core.SavePrice(newItem.Current_Price, string(newEntry.Hex()))

				newItem.UUID = string(newEntry.Hex())

				err = privateChanel.Publish(context.Background(),
					"status",
					newItem,
				)
				if err != nil {
					log.Fatal(err)
				}
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
