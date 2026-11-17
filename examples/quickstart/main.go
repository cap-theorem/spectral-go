package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	spectral "github.com/cap-theorem/spectral-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := spectral.New(spectral.Config{})
	if err != nil {
		log.Fatal(err)
	}

	lease, err := client.Register(ctx, spectral.Registration{
		Service:    "payments",
		InstanceID: "payments-1",
		Endpoint:   "http://127.0.0.1:9000",
		TTL:        10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("registered with %s, lease expires at %s", client.Addr(), lease.ExpiresAt)

	go func() {
		err := client.KeepAlive(ctx, lease)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("keepalive stopped: %v", err)
			stop()
		}
	}()

	inventory, err := client.Lookup(ctx, "inventory")
	switch {
	case errors.Is(err, spectral.ErrServiceNotFound):
		log.Print("no inventory instance registered yet")
	case err != nil:
		log.Fatal(err)
	default:
		log.Printf("inventory is at %s", inventory.Endpoint)
	}

	<-ctx.Done()
}
