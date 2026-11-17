package spectral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	Version     = "0.1.0"
	DefaultAddr = "http://127.0.0.1:8000"
	AddrEnv     = "SPECTRAL_ADDR"

	opRegister = "register"
	opRenew    = "renew"
	opLookup   = "lookup"
	opStatus   = "status"

	defaultTimeout = 10 * time.Second
	maxErrorBody   = 4096
)

type Config struct {
	Addr       string
	HTTPClient *http.Client
}

type Client struct {
	base *url.URL
	http *http.Client
}

func New(cfg Config) (*Client, error) {
	addr := cfg.Addr
	if addr == "" {
		addr = os.Getenv(AddrEnv)
	}
	if addr == "" {
		addr = DefaultAddr
	}

	base, err := url.Parse(strings.TrimRight(addr, "/"))
	if err != nil {
		return nil, fmt.Errorf("spectral: invalid addr %q: %w", addr, err)
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("spectral: addr %q must be an http(s) URL with a host", addr)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{base: base, http: httpClient}, nil
}

func (c *Client) Addr() string {
	return c.base.String()
}

func (c *Client) Register(ctx context.Context, reg Registration) (Lease, error) {
	body := registerRequest{
		Service:    reg.Service,
		InstanceID: reg.InstanceID,
		Endpoint:   reg.Endpoint,
		TTL:        reg.TTL.String(),
	}

	var resp leaseResponse
	if err := c.do(ctx, opRegister, http.MethodPost, "/register", nil, body, &resp); err != nil {
		return Lease{}, err
	}

	ttl, err := time.ParseDuration(resp.TTL)
	if err != nil {
		return Lease{}, fmt.Errorf("spectral register: invalid ttl %q in response: %w", resp.TTL, err)
	}

	return Lease{
		Token:     resp.Token,
		TTL:       ttl,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

func (c *Client) Renew(ctx context.Context, token string) error {
	return c.do(ctx, opRenew, http.MethodPost, "/renew", nil, renewRequest{Token: token}, nil)
}

func (c *Client) Lookup(ctx context.Context, service string) (Provider, error) {
	query := url.Values{"service": {service}}

	var resp providerResponse
	if err := c.do(ctx, opLookup, http.MethodGet, "/lookup", query, nil, &resp); err != nil {
		return Provider{}, err
	}

	return Provider{
		Service:    resp.Service,
		InstanceID: resp.InstanceID,
		Endpoint:   resp.Endpoint,
	}, nil
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var resp statusResponse
	if err := c.do(ctx, opStatus, http.MethodGet, "/status", nil, nil, &resp); err != nil {
		return Status{}, err
	}

	return Status{
		Ready: resp.Ready,
		State: State(resp.State),
	}, nil
}

func (c *Client) do(ctx context.Context, op, method, path string, query url.Values, in, out any) error {
	target := *c.base
	target.Path = c.base.Path + path
	target.RawQuery = query.Encode()

	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("spectral %s: encode request: %w", op, err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return fmt.Errorf("spectral %s: %w", op, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "spectral-go/"+Version)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("spectral %s: %w", op, ctxErr)
		}
		return fmt.Errorf("spectral %s: %w: %w", op, ErrSidecarUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &APIError{
			Op:         op,
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(msg)),
			Err:        errorKind(op, resp.StatusCode),
		}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("spectral %s: decode response: %w", op, err)
	}
	return nil
}
