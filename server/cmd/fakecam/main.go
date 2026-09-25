package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"camserver/internal/config"
	"camserver/internal/hub"
)

func main() {
	file := flag.String("file", "", "JPEG file to send on a loop")
	url := flag.String("url", "http://127.0.0.1:8080/ingest", "ingest URL")
	every := flag.Duration("interval", 200*time.Millisecond, "delay between frames")
	key := flag.String("key", "", "ingest API key (default: config.env)")
	flag.Parse()
	apiKey := *key
	if apiKey == "" {
		cfg, err := config.Load()
		if err != nil {
			log.Fatal(err)
		}
		apiKey = cfg.IngestAPIKey
	}
	if *file == "" {
		log.Fatal("need -file")
	}

	frame, err := os.ReadFile(*file)
	if err != nil {
		log.Fatal(err)
	}
	if !hub.ValidJPEG(frame) {
		log.Fatal("file is not a JPEG within the size limit")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := &http.Client{Timeout: 10 * time.Second}
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, *url, bytes.NewReader(frame))
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Set("Content-Type", "image/jpeg")
		req.Header.Set("X-API-Key", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("post: %v", err)
			if !sleep(ctx, 2*time.Second) {
				return
			}
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			log.Printf("post: status %d", resp.StatusCode)
		}
		if !sleep(ctx, *every) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
