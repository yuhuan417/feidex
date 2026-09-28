package codexrpc

import (
	"context"
	"encoding/json"
	"feidex/internal/config"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEventCallbackCanWaitForRPCResponse(t *testing.T) {
	c := New(config.CodexConfig{})
	defer c.Close()
	c.stdin = &recordingWriteCloser{}
	reader, writer := io.Pipe()
	defer writer.Close()
	c.stdout = reader
	c.waitDone = nil
	completed := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.SetHandlers(func(_ string, _ json.RawMessage) {
		completed <- c.Call(ctx, "thread/read", nil, nil)
	}, nil)
	go c.readLoop()
	if _, err := io.WriteString(writer, "{\"method\":\"turn/completed\",\"params\":{}}\n"); err != nil {
		t.Fatal(err)
	}
	// Wait for the callback to register its RPC before sending the response.
	for !strings.Contains(c.stdin.(*recordingWriteCloser).String(), "thread/read") {
		select {
		case <-ctx.Done():
			t.Fatal("callback did not start RPC")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := io.WriteString(writer, "{\"id\":1,\"result\":{}}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("reader blocked behind callback")
	}
}

func TestEventDispatchPreservesOrderAcrossEOF(t *testing.T) {
	c := New(config.CodexConfig{})
	defer c.Close()
	c.waitDone = nil
	events := make(chan string, 3)
	c.SetHandlers(func(method string, _ json.RawMessage) { events <- method }, func(RequestEnvelope) { events <- "request" })
	c.SetErrorHandler(func(error) { events <- "eof" })
	c.stdout = io.NopCloser(strings.NewReader("{\"id\":\"a\",\"method\":\"approval\"}\n{\"method\":\"serverRequest/resolved\"}\n"))
	go c.readLoop()
	for _, want := range []string{"request", "serverRequest/resolved", "eof"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("event missing")
		}
	}
}

func TestEventQueueOverflowFailsTransport(t *testing.T) {
	c := New(config.CodexConfig{})
	defer c.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	c.SetHandlers(func(string, json.RawMessage) { close(entered); <-release }, nil)
	failed := make(chan error, 1)
	c.SetErrorHandler(func(err error) { failed <- err })
	c.enqueueEvent(clientEvent{method: "blocked"})
	<-entered
	for i := 0; i <= cap(c.eventCh); i++ {
		c.enqueueEvent(clientEvent{method: "queued"})
	}
	select {
	case err := <-failed:
		if !strings.Contains(err.Error(), "overflow") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow did not fail transport")
	}
	select {
	case <-c.eventStop:
	default:
		t.Fatal("dispatcher not stopped")
	}
}

func TestCallbackRPCFailsPromptlyAfterEOF(t *testing.T) {
	c := New(config.CodexConfig{})
	defer c.Close()
	c.waitDone = nil
	c.stdin = &recordingWriteCloser{}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.SetHandlers(func(string, json.RawMessage) {
		close(entered)
		<-release
		done <- c.Call(ctx, "thread/read", nil, nil)
	}, nil)
	c.enqueueEvent(clientEvent{method: "turn/completed"})
	<-entered
	c.stdout = io.NopCloser(strings.NewReader(""))
	c.readLoop()
	close(release)
	select {
	case err := <-done:
		if err == nil || err == context.DeadlineExceeded {
			t.Fatalf("expected immediate transport failure, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal("callback RPC waited after EOF")
	}
}
