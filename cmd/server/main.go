package main

import (
	"flag"
	"log"

	"github.com/eyes/internal/api"
	"github.com/eyes/internal/config"
)

func main() {
	configPath := flag.String("config", "config.json", "config file path")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[main] Eyes Quant System starting...")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[main] load config: %v", err)
	}

	log.Printf("[main] config loaded: port=%s, bar_interval=%ds, window=%d",
		cfg.Server.Port, cfg.Feature.BarInterval, cfg.Feature.WindowSize)

	server := api.NewServer(cfg)
	if err := server.Start(); err != nil {
		log.Fatalf("[main] server error: %v", err)
	}
}
