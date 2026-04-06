package codexrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type Client struct {
	cmdPath string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser

	nextID    atomic.Int64
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan responseEnvelope

	onNotification func(string, json.RawMessage)
	onRequest      func(RequestEnvelope)
}

type responseEnvelope struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RequestEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type notificationEnvelope struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func New(cmdPath string) *Client {
	if cmdPath == "" {
		cmdPath = "codex"
	}
	return &Client{
		cmdPath: cmdPath,
		pending: map[int64]chan responseEnvelope{},
	}
}

func (c *Client) SetHandlers(onNotification func(string, json.RawMessage), onRequest func(RequestEnvelope)) {
	c.onNotification = onNotification
	c.onRequest = onRequest
}

func (c *Client) Start(ctx context.Context, experimentalAPI bool) error {
	c.cmd = exec.CommandContext(ctx, c.cmdPath, "app-server")
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	c.cmd.Stderr = os.Stderr
	c.stdin = stdin
	c.stdout = stdout
	if err := c.cmd.Start(); err != nil {
		return err
	}
	go c.readLoop()

	var initResp struct {
		UserAgent      string `json:"userAgent"`
		CodexHome      string `json:"codexHome"`
		PlatformFamily string `json:"platformFamily"`
		PlatformOS     string `json:"platformOs"`
	}
	if err := c.Call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "feidex",
			"title":   "Feidex Feishu Middleware",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": experimentalAPI,
			"optOutNotificationMethods": []string{
				"item/reasoning/textDelta",
				"rawResponseItem/completed",
			},
		},
	}, &initResp); err != nil {
		return err
	}
	return c.Notify("initialized", map[string]any{})
}

func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan responseEnvelope, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.send(map[string]any{"method": method, "id": id, "params": params}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("%s", resp.Error.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (c *Client) Notify(method string, params any) error {
	return c.send(map[string]any{"method": method, "params": params})
}

func (c *Client) Reply(id json.RawMessage, result any) error {
	return c.send(map[string]any{"id": json.RawMessage(id), "result": result})
}

func (c *Client) ReplyError(id json.RawMessage, code int, message string) error {
	return c.send(map[string]any{
		"id": id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (c *Client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.stdin == nil {
		return errors.New("client not started")
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		if rawID, ok := obj["id"]; ok {
			if _, hasMethod := obj["method"]; hasMethod {
				var req RequestEnvelope
				if err := json.Unmarshal(line, &req); err == nil && c.onRequest != nil {
					go c.onRequest(req)
				}
				continue
			}
			var id int64
			if err := json.Unmarshal(rawID, &id); err == nil {
				var resp responseEnvelope
				if err := json.Unmarshal(line, &resp); err == nil {
					c.pendingMu.Lock()
					ch := c.pending[id]
					delete(c.pending, id)
					c.pendingMu.Unlock()
					if ch != nil {
						ch <- resp
					}
				}
			}
			continue
		}
		var notif notificationEnvelope
		if err := json.Unmarshal(line, &notif); err == nil && notif.Method != "" && c.onNotification != nil {
			go c.onNotification(notif.Method, notif.Params)
		}
	}
}
