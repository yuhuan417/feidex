package feishu

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// jsonResponse builds a stub HTTP response for the endpoint preflight.
func jsonResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestAdapterStartInitializesWithoutBlocking(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			return jsonResponse(req, `{"code":1}`), nil
		case "/callback/ws/endpoint":
			return jsonResponse(req, `{"code":999,"msg":"bad auth"}`), nil
		default:
			return jsonResponse(req, `{"code":999}`), nil
		}
	})
	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	a.SetHandlers(func(*InboundMessage) {}, func(*CardAction) (*callback.CardActionTriggerResponse, error) {
		return &callback.CardActionTriggerResponse{}, nil
	}, func(*MessageRecall) {}, func(*MessageReaction) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("Start() error = %v, want auth failure", err)
	}
	time.Sleep(20 * time.Millisecond)
	if a.wsClient != nil {
		t.Fatal("expected ws client to stay nil after startup failure")
	}
	if a.feishuChannel != nil {
		t.Fatal("expected channel to stay nil after startup failure")
	}
	a.Stop()
}

// A successful preflight must leave the transport wired up. The SDK client
// itself connects in the background; this only asserts the wiring, since the
// connection lifecycle now belongs to the SDK.
func TestAdapterStartSuccessWiresChannelRuntime(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			return jsonResponse(req, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
		case "/callback/ws/endpoint":
			return jsonResponse(req, `{"code":0,"data":{"URL":"wss://example.test/ws"}}`), nil
		default:
			return jsonResponse(req, `{"code":0}`), nil
		}
	})

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start(success) error = %v", err)
	}
	if a.wsClient == nil {
		t.Fatal("expected ws client to be initialized")
	}
	if a.feishuChannel == nil {
		t.Fatal("expected channel to be initialized")
	}
	// Stop must be safe to call and must not block the caller.
	done := make(chan struct{})
	go func() {
		a.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked")
	}
}

func TestFetchWSEndpointErrors(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`unavailable`)),
			Request:    req,
		}, nil
	})
	if _, err := a.fetchWSEndpoint(context.Background()); err == nil || !strings.Contains(err.Error(), "status=503") {
		t.Fatalf("fetchWSEndpoint(status error) = %v", err)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"code":1,"msg":"busy"}`), nil
	})
	if _, err := a.fetchWSEndpoint(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("fetchWSEndpoint(system busy) = %v", err)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"code":0,"data":{}}`), nil
	})
	if _, err := a.fetchWSEndpoint(context.Background()); err == nil || !strings.Contains(err.Error(), "empty URL") {
		t.Fatalf("fetchWSEndpoint(empty url) = %v", err)
	}
}

// The preflight must surface the server-provided client config, because the SDK
// applies it without guarding against zero: a missing value would overwrite the
// SDK's defaults with 0 and cause reconnect churn. See
// docs/oapi-sdk-v3.12.0-upgrade-plan.md 3.5.5.
func TestFetchWSEndpointParsesClientConfig(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"code":0,"data":{"URL":"wss://example.test/ws","ClientConfig":{"PingInterval":120,"ReconnectInterval":5,"ReconnectCount":-1,"ReconnectNonce":30}}}`), nil
	})

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	resp, err := a.fetchWSEndpoint(context.Background())
	if err != nil {
		t.Fatalf("fetchWSEndpoint() error = %v", err)
	}
	conf := resp.Data.ClientConfig
	if conf == nil {
		t.Fatal("expected ClientConfig to be parsed")
	}
	if conf.PingInterval != 120 || conf.ReconnectInterval != 5 {
		t.Fatalf("ClientConfig = %+v, want ping=120 reconnect=5", conf)
	}
}

// The preflight validates credentials only — it must not dial. A dial failure
// is transient and the SDK reconnects through it; failing startup for it would
// take the whole daemon down.
func TestValidateWSStartupDoesNotDial(t *testing.T) {
	origTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = origTransport })
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, `{"code":0,"data":{"URL":"wss://127.0.0.1:1/never-listened"}}`), nil
	})

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	if err := a.validateWSStartup(context.Background()); err != nil {
		t.Fatalf("validateWSStartup() = %v, want nil for an unreachable URL (no dial expected)", err)
	}
}
