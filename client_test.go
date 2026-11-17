package spectral_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	spectral "github.com/cap-theorem/spectral-go"
)

func newClient(t *testing.T, handler http.HandlerFunc) *spectral.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := spectral.New(spectral.Config{Addr: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestNewAddrResolution(t *testing.T) {
	tests := []struct {
		name string
		addr string
		env  string
		want string
	}{
		{name: "default", want: spectral.DefaultAddr},
		{name: "env", env: "http://10.0.0.5:8007", want: "http://10.0.0.5:8007"},
		{name: "explicit beats env", addr: "http://127.0.0.1:8003", env: "http://10.0.0.5:8007", want: "http://127.0.0.1:8003"},
		{name: "trailing slash", addr: "http://127.0.0.1:8003/", want: "http://127.0.0.1:8003"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(spectral.AddrEnv, tt.env)
			client, err := spectral.New(spectral.Config{Addr: tt.addr})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := client.Addr(); got != tt.want {
				t.Fatalf("Addr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRejectsBadAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8000", "ftp://127.0.0.1:8000", "http://", "://bad"} {
		t.Run(addr, func(t *testing.T) {
			if _, err := spectral.New(spectral.Config{Addr: addr}); err == nil {
				t.Fatalf("New(%q) succeeded, want error", addr)
			}
		})
	}
}

func TestRegister(t *testing.T) {
	expires := time.Date(2026, 11, 16, 12, 0, 10, 0, time.UTC)

	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/register" {
			t.Errorf("got %s %s, want POST /register", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		want := map[string]string{
			"service":     "payments",
			"instance_id": "payments-1",
			"endpoint":    "http://100.64.1.3:9007",
			"ttl":         "10s",
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("body[%q] = %q, want %q", k, body[k], v)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "abc123",
			"ttl":        "10s",
			"expires_at": expires,
		})
	})

	lease, err := client.Register(context.Background(), spectral.Registration{
		Service:    "payments",
		InstanceID: "payments-1",
		Endpoint:   "http://100.64.1.3:9007",
		TTL:        10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if lease.Token != "abc123" || lease.TTL != 10*time.Second || !lease.ExpiresAt.Equal(expires) {
		t.Fatalf("lease = %+v", lease)
	}
}

func TestRegisterErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusBadRequest, want: spectral.ErrInvalidRegistration},
		{status: http.StatusConflict, want: spectral.ErrAlreadyRegistered},
		{status: http.StatusGatewayTimeout, want: spectral.ErrSidecarTimeout},
		{status: http.StatusInternalServerError, want: nil},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", tt.status)
			})

			_, err := client.Register(context.Background(), spectral.Registration{TTL: time.Second})

			var apiErr *spectral.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.StatusCode != tt.status || apiErr.Message != "nope" || apiErr.Op != "register" {
				t.Fatalf("apiErr = %+v", apiErr)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && apiErr.Err != nil {
				t.Fatalf("apiErr.Err = %v, want nil", apiErr.Err)
			}
		})
	}
}

func TestRenew(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/renew" {
			t.Errorf("got %s %s, want POST /renew", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["token"] != "abc123" {
			t.Errorf("token = %q", body["token"])
		}
		w.WriteHeader(http.StatusAccepted)
	})

	if err := client.Renew(context.Background(), "abc123"); err != nil {
		t.Fatalf("Renew: %v", err)
	}
}

func TestRenewUnknownToken(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unknown lease", http.StatusNotFound)
	})

	err := client.Renew(context.Background(), "gone")
	if !errors.Is(err, spectral.ErrLeaseExpired) {
		t.Fatalf("err = %v, want ErrLeaseExpired", err)
	}
}

func TestLookup(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/lookup" {
			t.Errorf("got %s %s, want GET /lookup", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("service"); got != "model-server/llama 70b" {
			t.Errorf("service = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service":     "model-server/llama 70b",
			"instance_id": "ms-0412",
			"endpoint":    "http://100.64.1.6:9002",
		})
	})

	p, err := client.Lookup(context.Background(), "model-server/llama 70b")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	want := spectral.Provider{Service: "model-server/llama 70b", InstanceID: "ms-0412", Endpoint: "http://100.64.1.6:9002"}
	if p != want {
		t.Fatalf("provider = %+v, want %+v", p, want)
	}
}

func TestLookupNotFound(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no providers", http.StatusNotFound)
	})

	_, err := client.Lookup(context.Background(), "inventory")
	if !errors.Is(err, spectral.ErrServiceNotFound) {
		t.Fatalf("err = %v, want ErrServiceNotFound", err)
	}
}

func TestStatus(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/status" {
			t.Errorf("got %s %s, want GET /status", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "state": "active"})
	})

	st, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Ready || st.State != spectral.StateActive {
		t.Fatalf("status = %+v", st)
	}
}

func TestBasePathPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sidecar/status" {
			t.Errorf("path = %q, want /sidecar/status", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "state": "active"})
	}))
	t.Cleanup(srv.Close)

	client, err := spectral.New(spectral.Config{Addr: srv.URL + "/sidecar/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
}

func TestSidecarUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()

	client, err := spectral.New(spectral.Config{Addr: addr})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Status(context.Background())
	if !errors.Is(err, spectral.ErrSidecarUnavailable) {
		t.Fatalf("err = %v, want ErrSidecarUnavailable", err)
	}
}

func TestContextCanceled(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Status(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errors.Is(err, spectral.ErrSidecarUnavailable) {
		t.Fatalf("canceled call reported as ErrSidecarUnavailable: %v", err)
	}
}
