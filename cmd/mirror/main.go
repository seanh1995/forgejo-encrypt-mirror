package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/seanh1995/forgejo-encrypt-mirror/internal/config"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/queue"
	"github.com/seanh1995/forgejo-encrypt-mirror/internal/server"
)

func main() {

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("starting forgejo encrypt mirror")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	jobQueue := queue.New(100)

	pool := queue.NewPool(jobQueue, 3, func(ctx context.Context, job queue.Job) error {
		log.Printf("processing job %s: %s/%s@%s (%s)", job.ID, job.Owner, job.Repo, job.Branch, job.Commit)
		// TODO: replace with git engine + encryption pipeline (Phase 4/5).
		return nil
	})
	pool.Start()
	defer pool.Stop()

	err = server.Start(ctx, server.Config{
		Address:       cfg.Server.Address,
		WebhookSecret: cfg.Forgejo.WebhookSecret,
		Queue:         jobQueue,
	})
	if err != nil {
		log.Fatal(err)
	}
}