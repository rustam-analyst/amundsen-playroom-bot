// Точка входа: собирает конфиг, хранилище, сервис и бота, запускает polling.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"playroom-bot/internal/booking"
	"playroom-bot/internal/config"
	"playroom-bot/internal/storage/sqlite"
	"playroom-bot/internal/telegram"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()

	service := booking.NewService(store)

	bot, err := telegram.New(cfg.BotToken, cfg.Debug, service)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("bot: %v", err)
	}
}
