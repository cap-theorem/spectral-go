package spectral_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	spectral "github.com/cap-theorem/spectral-go"
)

func TestKeepAliveRenewsUntilCanceled(t *testing.T) {
	renewed := make(chan struct{}, 16)
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		renewed <- struct{}{}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.KeepAlive(ctx, spectral.Lease{Token: "abc123", TTL: 60 * time.Millisecond})
	}()

	for i := range 3 {
		select {
		case <-renewed:
		case <-time.After(2 * time.Second):
			t.Fatalf("renew %d never arrived", i+1)
		}
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("KeepAlive = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("KeepAlive did not return after cancel")
	}
}

func TestKeepAliveStopsWhenLeaseRejected(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown lease", http.StatusNotFound)
	})

	err := runKeepAlive(t, client, spectral.Lease{Token: "gone", TTL: 60 * time.Millisecond})
	if !errors.Is(err, spectral.ErrLeaseExpired) {
		t.Fatalf("KeepAlive = %v, want ErrLeaseExpired", err)
	}
}

func TestKeepAliveGivesUpAfterTTL(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	start := time.Now()
	err := runKeepAlive(t, client, spectral.Lease{Token: "abc123", TTL: 60 * time.Millisecond})
	if !errors.Is(err, spectral.ErrLeaseExpired) {
		t.Fatalf("KeepAlive = %v, want ErrLeaseExpired", err)
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("gave up after %s, before the lease ttl", elapsed)
	}

	var apiErr *spectral.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("KeepAlive error does not carry the last renew failure: %v", err)
	}
}

func TestKeepAliveRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	renewed := make(chan struct{}, 16)
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "blip", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		renewed <- struct{}{}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.KeepAlive(ctx, spectral.Lease{Token: "abc123", TTL: 60 * time.Millisecond})
	}()

	select {
	case <-renewed:
	case err := <-done:
		t.Fatalf("KeepAlive returned %v after one transient failure", err)
	case <-time.After(2 * time.Second):
		t.Fatal("no successful renew after transient failure")
	}
}

func TestKeepAliveRejectsTinyTTL(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected request")
	})

	if err := client.KeepAlive(context.Background(), spectral.Lease{Token: "abc123", TTL: 2}); err == nil {
		t.Fatal("KeepAlive accepted a 2ns ttl")
	}
}

func runKeepAlive(t *testing.T, client *spectral.Client, lease spectral.Lease) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- client.KeepAlive(context.Background(), lease)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("KeepAlive did not return")
		return nil
	}
}
