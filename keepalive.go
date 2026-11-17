package spectral

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (c *Client) KeepAlive(ctx context.Context, lease Lease) error {
	interval := lease.TTL / 3
	if interval <= 0 {
		return fmt.Errorf("spectral keepalive: lease ttl %s is too short", lease.TTL)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(lease.TTL)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			err := c.Renew(ctx, lease.Token)
			switch {
			case err == nil:
				deadline = now.Add(lease.TTL)
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, ErrLeaseExpired):
				return err
			case !now.Before(deadline):
				return fmt.Errorf("spectral keepalive: %w: last renew failed: %w", ErrLeaseExpired, err)
			}
		}
	}
}
