# spectral-go

Go client for the Spectral sidecar.

Every service instance runs next to a Spectral sidecar. Your service uses this client to talk to its own sidecar over HTTP: register itself, keep its lease alive, and look up other services. The sidecar handles everything else (gossip, failure detection, keeping the directory in sync across machines).

## Install

```sh
go get github.com/cap-theorem/spectral-go
```

Requires Go 1.22+. No dependencies outside the standard library.

## Quickstart

```go
client, err := spectral.New(spectral.Config{})
if err != nil {
	log.Fatal(err)
}

lease, err := client.Register(ctx, spectral.Registration{
	Service:    "payments",
	InstanceID: "payments-1",
	Endpoint:   "http://100.64.1.3:9007",
	TTL:        10 * time.Second,
})
if err != nil {
	log.Fatal(err)
}

go client.KeepAlive(ctx, lease)

inventory, err := client.Lookup(ctx, "inventory")
if errors.Is(err, spectral.ErrServiceNotFound) {
	// nobody is serving inventory right now
}
resp, err := http.Get(inventory.Endpoint + "/stock")
```

A runnable version is in [`examples/quickstart`](examples/quickstart/main.go).

## Connecting to the sidecar

`spectral.New` picks the sidecar address in this order:

1. `Config.Addr`, if set
2. the `SPECTRAL_ADDR` environment variable
3. `http://127.0.0.1:8000`, the sidecar's default API port

Pass `Config.HTTPClient` to use your own `*http.Client`. The default has a 10s timeout. Every method also respects its `context.Context`.

## API

| Method | What it does |
|---|---|
| `New(Config) (*Client, error)` | Create a client for one sidecar. |
| `Register(ctx, Registration) (Lease, error)` | Announce this instance. The sidecar spreads it to the whole network. |
| `Renew(ctx, token) error` | Extend the lease once. |
| `KeepAlive(ctx, Lease) error` | Renew every TTL/3 until `ctx` is canceled. Blocks; run it in a goroutine. Retries failed renewals and returns `ErrLeaseExpired` if none succeed for a full TTL, or right away if the sidecar no longer knows the lease. |
| `Lookup(ctx, service) (Provider, error)` | Get one healthy instance of a service, answered from the sidecar's local copy of the directory. |
| `Status(ctx) (Status, error)` | Whether the sidecar is ready, and its state. |

If your instance stops renewing (crash, hang, `KeepAlive` stopped), the lease expires and the instance disappears from every sidecar's directory. There is no explicit deregister: stop renewing, or shut the sidecar down.

## Errors

Check errors with `errors.Is`:

| Error | Meaning |
|---|---|
| `ErrInvalidRegistration` | The sidecar rejected the registration (missing fields, bad endpoint URL, non-positive TTL). |
| `ErrAlreadyRegistered` | This registration already exists. |
| `ErrServiceNotFound` | No healthy instance of that service is known. |
| `ErrLeaseExpired` | The lease is gone; register again. |
| `ErrSidecarTimeout` | The sidecar accepted the request but did not answer in time. |
| `ErrSidecarUnavailable` | The sidecar could not be reached at all. |

Any non-2xx response is also an `*APIError` with the HTTP status code and the sidecar's message, available through `errors.As`.

## HTTP contract

This is the sidecar API the client speaks. Every Spectral SDK uses the same contract.

| Call | Request | Success | Errors |
|---|---|---|---|
| `POST /register` | `{"service", "instance_id", "endpoint", "ttl": "10s"}` | `201 {"token", "ttl", "expires_at"}` | `400` invalid, `409` exists, `504` timeout |
| `POST /renew` | `{"token"}` | `202` | `404` unknown or expired lease, `504` timeout |
| `GET /lookup?service=<name>` | | `200 {"service", "instance_id", "endpoint"}` | `404` no provider, `504` timeout |
| `GET /status` | | `200 {"ready", "state"}` | `504` timeout |

Durations are Go duration strings (`"10s"`, `"1m30s"`). Timestamps are RFC 3339. Error bodies are plain text.
