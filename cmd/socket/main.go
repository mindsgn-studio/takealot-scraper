package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ably/ably-go/ably"
	"github.com/joho/godotenv"
	"github.com/mindsgn-studio/takealot-scraper/internal/core"
)

const (
	workerCount       = 5
	jobQueueSize      = 50
	ablyConnectTimout = 15 * time.Second
)

type job struct {
	clientID string
	url      string
	ctx      context.Context
}

func main() {
	logger := log.New(os.Stdout, "[tracker] ", log.LstdFlags|log.Lmsgprefix)
	_ = godotenv.Load()

	ablyKey := os.Getenv("ABLY_KEY")
	if ablyKey == "" {
		logger.Fatal("ABLY_KEY missing from environment")
	}

	client, err := ably.NewRealtime(
		ably.WithKey(ablyKey),
		ably.WithClientID("local-server"),
	)
	if err != nil {
		logger.Fatalf("failed to create ably client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	if err := waitForConnection(ctx, client, ablyConnectTimout); err != nil {
		logger.Fatalf("failed to connect to ably: %v", err)
	}
	logger.Println("connected to ably")

	itemsCh := client.Channels.Get("items")

	jobs := make(chan job, jobQueueSize)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker(ctx, &wg, client, logger, jobs)
	}

	unsubscribe, err := itemsCh.SubscribeAll(ctx, func(msg *ably.Message) {
		url := ""
		switch v := msg.Data.(type) {
		case string:
			url = v
		case []byte:
			url = string(v)
		default:
			logger.Printf("unsupported msg.Data type: %T (client=%s)", msg.Data, msg.ClientID)
			return
		}

		select {
		case jobs <- job{clientID: msg.ClientID, url: url, ctx: ctx}:
		default:
			logger.Printf("job queue full, rejecting message from client=%s", msg.ClientID)
		}
	})
	if err != nil {
		logger.Fatalf("failed to subscribe to items channel: %v", err)
	}

	<-quit
	logger.Println("shutdown requested")

	unsubscribe()
	cancel()
	close(jobs)
	wg.Wait()
	logger.Println("all workers stopped; exiting")
}

func waitForConnection(ctx context.Context, client *ably.Realtime, timeout time.Duration) error {
	connCh := make(chan ably.ConnectionStateChange, 1)
	client.Connection.On(ably.ConnectionEventConnected, func(change ably.ConnectionStateChange) {
		select {
		case connCh <- change:
		default:
		}
	})

	select {
	case <-connCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for ably connection")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func worker(ctx context.Context, wg *sync.WaitGroup, client *ably.Realtime, logger *log.Logger, jobs <-chan job) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-jobs:
			if !ok {
				return
			}
			if err := processJob(j, client, logger); err != nil {
				logger.Printf("job error (client=%s, url=%s): %v", j.clientID, j.url, err)
			}
		}
	}
}

func processJob(j job, client *ably.Realtime, logger *log.Logger) error {
	opCtx, cancel := context.WithTimeout(j.ctx, 30*time.Second)
	defer cancel()

	channelName := "private:" + j.clientID

	publishStatus := func(status string, detail any) {
		privateCh := client.Channels.Get(channelName)
		_ = privateCh.Publish(opCtx, "status", map[string]any{
			"status": status,
			"detail": detail,
			"time":   time.Now().UTC(),
		})
	}

	item, err := core.OpenPageTakealot(j.url)
	if err != nil {
		publishStatus("error", map[string]any{
			"step":    "scrape",
			"message": fmt.Sprintf("failed to open page: %v", err),
			"url":     j.url,
		})
		return fmt.Errorf("open page: %w", err)
	}

	entry, err := core.SaveItemData(item)
	if err != nil {
		publishStatus("error", map[string]any{
			"step":    "save_item",
			"message": fmt.Sprintf("failed to save item data: %v", err),
			"url":     j.url,
		})
		return fmt.Errorf("save item data: %w", err)
	}

	item.UUID = entry.Hex()

	if err := core.SavePrice(item.Current_Price, entry.Hex()); err != nil {
		publishStatus("error", map[string]any{
			"step":    "save_price",
			"message": fmt.Sprintf("failed to save price: %v", err),
		})
		return fmt.Errorf("save price: %w", err)
	}

	publishStatus("ok", item)

	return nil
}
