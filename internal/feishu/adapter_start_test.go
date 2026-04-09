package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"feidex/internal/config"

	gws "github.com/gorilla/websocket"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func TestAdapterStartInitializesWithoutBlocking(t *testing.T) {
	origTransport := http.DefaultTransport
	origDial := wsDialContext
	origRunner := wsClientRunner
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
		wsDialContext = origDial
		wsClientRunner = origRunner
	})
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":1}`)),
				Request:    req,
			}, nil
		case "/callback/ws/endpoint":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":999,"msg":"bad auth"}`)),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":999}`)),
				Request:    req,
			}, nil
		}
	})
	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	a.SetHandlers(func(*InboundMessage) {}, func(*CardAction) (*callback.CardActionTriggerResponse, error) {
		return &callback.CardActionTriggerResponse{}, nil
	}, func(*BotMenuClick) {}, func(*MessageRecall) {}, func(*MessageReaction) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("Start() error = %v, want auth failure", err)
	}
	time.Sleep(20 * time.Millisecond)
	if a.wsClient != nil {
		t.Fatal("expected ws client to stay nil after startup failure")
	}
	a.Stop()
}

func TestAdapterStartSuccessAndWSValidationBranches(t *testing.T) {
	origTransport := http.DefaultTransport
	origDial := wsDialContext
	origRunner := wsClientRunner
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
		wsDialContext = origDial
		wsClientRunner = origRunner
	})

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":0,"tenant_access_token":"tenant-token","expire":7200}`)),
				Request:    req,
			}, nil
		case "/callback/ws/endpoint":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"URL":"wss://example.test/ws"}}`)),
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
				Request:    req,
			}, nil
		}
	})
	wsDialContext = func(ctx context.Context, urlStr string, header http.Header) (*gws.Conn, *http.Response, error) {
		_ = ctx
		_ = urlStr
		_ = header
		return nil, nil, nil
	}
	started := make(chan struct{}, 1)
	wsClientRunner = func(client *larkws.Client, ctx context.Context) {
		_ = client
		_ = ctx
		started <- struct{}{}
	}

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start(success) error = %v", err)
	}
	if a.wsClient == nil {
		t.Fatal("expected ws client to be initialized")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected ws runner to be invoked")
	}
}

func TestFetchWSEndpointURLAndValidateWSStartupErrors(t *testing.T) {
	origTransport := http.DefaultTransport
	origDial := wsDialContext
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
		wsDialContext = origDial
	})

	a := New(config.FeishuConfig{AppID: "app", AppSecret: "secret"})

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`unavailable`)),
			Request:    req,
		}, nil
	})
	if _, err := a.fetchWSEndpointURL(context.Background()); err == nil || !strings.Contains(err.Error(), "status=503") {
		t.Fatalf("fetchWSEndpointURL(status error) = %v", err)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":1,"msg":"busy"}`)),
			Request:    req,
		}, nil
	})
	if _, err := a.fetchWSEndpointURL(context.Background()); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("fetchWSEndpointURL(system busy) = %v", err)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{}}`)),
			Request:    req,
		}, nil
	})
	if _, err := a.fetchWSEndpointURL(context.Background()); err == nil || !strings.Contains(err.Error(), "empty URL") {
		t.Fatalf("fetchWSEndpointURL(empty url) = %v", err)
	}

	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"URL":"wss://example.test/ws"}}`)),
			Request:    req,
		}, nil
	})
	wsDialContext = func(ctx context.Context, urlStr string, header http.Header) (*gws.Conn, *http.Response, error) {
		_ = ctx
		_ = urlStr
		_ = header
		return nil, &http.Response{StatusCode: http.StatusUnauthorized}, errors.New("bad handshake")
	}
	if err := a.validateWSStartup(context.Background()); err == nil || !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("validateWSStartup(handshake) = %v", err)
	}
}
