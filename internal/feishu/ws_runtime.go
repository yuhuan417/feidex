package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gws "github.com/gorilla/websocket"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

var wsSetWriteDeadline = func(conn *gws.Conn, deadline time.Time) error {
	return conn.SetWriteDeadline(deadline)
}

var wsWriteFrame = func(conn *gws.Conn, messageType int, data []byte) error {
	return conn.WriteMessage(messageType, data)
}

var wsCloseConn = func(conn *gws.Conn) error {
	return conn.Close()
}

var wsWallNow = func() time.Time {
	return time.Now().Round(0)
}

type wsLivenessAction int

const (
	wsLivenessActionNone wsLivenessAction = iota
	wsLivenessActionProbe
	wsLivenessActionReconnect
)

func (a *Adapter) runWSLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			a.closeWSConn()
			return
		}
		hadSession, err := a.runWSConnection(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Warn("feishu websocket disconnected", "error", err)
		}
		if ctx.Err() != nil {
			a.closeWSConn()
			return
		}
		if delay := wsReconnectDelayForExit(hadSession, a.currentWSReconnectInterval()); delay <= 0 {
			continue
		} else if !sleepWithContext(ctx, delay) {
			a.closeWSConn()
			return
		}
	}
}

func (a *Adapter) runWSConnection(ctx context.Context) (bool, error) {
	hadSession := false
	endpointURL, err := a.fetchWSEndpointURL(ctx)
	if err != nil {
		return hadSession, err
	}
	conn, resp, err := wsDialContext(ctx, endpointURL, nil)
	if err != nil {
		if resp != nil {
			return hadSession, fmt.Errorf("feishu websocket connect failed: status=%d: %w", resp.StatusCode, err)
		}
		return hadSession, fmt.Errorf("feishu websocket connect failed: %w", err)
	}
	if conn == nil {
		return hadSession, fmt.Errorf("feishu websocket connect returned nil connection")
	}
	u, err := url.Parse(endpointURL)
	if err != nil {
		_ = conn.Close()
		return hadSession, err
	}
	serviceID, _ := strconv.ParseInt(u.Query().Get(larkws.ServiceID), 10, 32)
	a.storeWSConn(conn, int32(serviceID))
	hadSession = true
	defer a.closeWSConn()
	slog.Info("feishu websocket connected",
		"conn_id", strings.TrimSpace(u.Query().Get(larkws.DeviceID)),
		"service_id", int32(serviceID),
	)

	errCh := make(chan error, 2)
	go a.wsPingLoop(ctx, errCh)
	go a.wsReadLoop(ctx, conn, errCh)
	go a.wsLivenessMonitor(ctx, errCh)
	select {
	case <-ctx.Done():
		return hadSession, ctx.Err()
	case err := <-errCh:
		return hadSession, err
	}
}

func (a *Adapter) wsPingLoop(ctx context.Context, errCh chan<- error) {
	for {
		interval := a.currentWSPingInterval()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := a.writeWSPing(); err != nil {
			a.failWSLoop(errCh, err)
			return
		}
	}
}

func (a *Adapter) wsReadLoop(ctx context.Context, conn *gws.Conn, errCh chan<- error) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(a.currentWSReadTimeout())); err != nil {
			a.failWSLoop(errCh, fmt.Errorf("set websocket read deadline: %w", err))
			return
		}
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			a.failWSLoop(errCh, err)
			return
		}
		a.noteWSInboundActivity(time.Now())
		if mt != gws.BinaryMessage {
			slog.Warn("feishu websocket ignored non-binary message", "message_type", mt)
			continue
		}
		if err := a.handleWSFrame(ctx, msg); err != nil {
			a.failWSLoop(errCh, err)
			return
		}
	}
}

func (a *Adapter) handleWSFrame(ctx context.Context, msg []byte) error {
	var frame larkws.Frame
	if err := frame.Unmarshal(msg); err != nil {
		return fmt.Errorf("unmarshal websocket frame: %w", err)
	}
	switch larkws.FrameType(frame.Method) {
	case larkws.FrameTypeControl:
		return a.handleWSControlFrame(frame)
	case larkws.FrameTypeData:
		return a.handleWSDataFrame(ctx, frame)
	default:
		return nil
	}
}

func (a *Adapter) handleWSControlFrame(frame larkws.Frame) error {
	hs := larkws.Headers(frame.Headers)
	if larkws.MessageType(hs.GetString(larkws.HeaderType)) != larkws.MessageTypePong {
		return nil
	}
	if len(frame.Payload) == 0 {
		return nil
	}
	conf := &larkws.ClientConfig{}
	if err := json.Unmarshal(frame.Payload, conf); err != nil {
		return fmt.Errorf("unmarshal websocket client config: %w", err)
	}
	a.applyWSClientConfig(conf)
	return nil
}

func (a *Adapter) handleWSDataFrame(ctx context.Context, frame larkws.Frame) error {
	hs := larkws.Headers(frame.Headers)
	sum := hs.GetInt(larkws.HeaderSum)
	seq := hs.GetInt(larkws.HeaderSeq)
	msgID := hs.GetString(larkws.HeaderMessageID)
	traceID := hs.GetString(larkws.HeaderTraceID)
	typeName := hs.GetString(larkws.HeaderType)

	pl := frame.Payload
	if sum > 1 {
		pl = a.combineWSMessage(msgID, sum, seq, pl)
		if pl == nil {
			return nil
		}
	}

	var (
		respPayload []byte
		resp        = larkws.NewResponseByCode(http.StatusOK)
		err         error
		rsp         any
	)
	switch larkws.MessageType(typeName) {
	case larkws.MessageTypeEvent:
		if a.wsDispatcher == nil {
			err = fmt.Errorf("missing websocket dispatcher")
		} else {
			rsp, err = a.wsDispatcher.Do(ctx, pl)
		}
	case larkws.MessageTypeCard:
		return nil
	default:
		return nil
	}
	if err != nil {
		slog.Warn("feishu websocket handler failed", "message_id", msgID, "trace_id", traceID, "error", err)
		resp = larkws.NewResponseByCode(http.StatusInternalServerError)
	} else if rsp != nil {
		respPayload, err = json.Marshal(rsp)
		if err != nil {
			slog.Warn("feishu websocket response marshal failed", "message_id", msgID, "trace_id", traceID, "error", err)
			resp = larkws.NewResponseByCode(http.StatusInternalServerError)
		} else {
			resp.Data = respPayload
		}
	}
	encodedResp, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal websocket response: %w", err)
	}
	hs.Add(larkws.HeaderBizRt, "0")
	frame.Payload = encodedResp
	frame.Headers = hs
	out, err := frame.Marshal()
	if err != nil {
		return fmt.Errorf("marshal websocket reply frame: %w", err)
	}
	if err := a.writeWSMessage(gws.BinaryMessage, out); err != nil {
		return fmt.Errorf("write websocket reply frame: %w", err)
	}
	return nil
}

func (a *Adapter) applyWSClientConfig(conf *larkws.ClientConfig) {
	if conf == nil {
		return
	}
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	if conf.PingInterval > 0 {
		a.wsPingInterval = time.Duration(conf.PingInterval) * time.Second
	}
	if conf.ReconnectInterval > 0 {
		a.wsReconnectInterval = time.Duration(conf.ReconnectInterval) * time.Second
	}
}

func (a *Adapter) combineWSMessage(msgID string, sum, seq int, payload []byte) []byte {
	if a == nil || a.wsFragments == nil || sum <= 1 || strings.TrimSpace(msgID) == "" || seq < 0 || seq >= sum {
		return payload
	}
	cached := a.wsFragments.Get(msgID)
	if cached == nil {
		parts := make([][]byte, sum)
		parts[seq] = append([]byte(nil), payload...)
		a.wsFragments.Set(msgID, parts, wsFragmentCacheTTL)
		return nil
	}
	parts, _ := cached.([][]byte)
	if len(parts) != sum {
		parts = make([][]byte, sum)
	}
	parts[seq] = append([]byte(nil), payload...)
	complete := true
	total := 0
	for _, part := range parts {
		if part == nil {
			complete = false
			break
		}
		total += len(part)
	}
	if !complete {
		a.wsFragments.Set(msgID, parts, wsFragmentCacheTTL)
		return nil
	}
	merged := make([]byte, 0, total)
	for _, part := range parts {
		merged = append(merged, part...)
	}
	a.wsFragments.Set(msgID, []byte{}, time.Nanosecond)
	return merged
}

func (a *Adapter) currentWSPingInterval() time.Duration {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	if a.wsPingInterval <= 0 {
		return wsDefaultPingInterval
	}
	return a.wsPingInterval
}

func (a *Adapter) currentWSReconnectInterval() time.Duration {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	interval := a.wsReconnectInterval
	if interval <= 0 {
		interval = wsDefaultReconnectInterval
	}
	if interval > wsMaxReconnectInterval {
		return wsMaxReconnectInterval
	}
	return interval
}

func (a *Adapter) currentWSReadTimeout() time.Duration {
	interval := a.currentWSPingInterval()*2 + 30*time.Second
	if interval < wsMinReadTimeout {
		return wsMinReadTimeout
	}
	return interval
}

func (a *Adapter) currentWSWriteTimeout() time.Duration {
	return wsDefaultWriteTimeout
}

func (a *Adapter) currentWSServiceID() int32 {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	return a.wsServiceID
}

func (a *Adapter) storeWSConn(conn *gws.Conn, serviceID int32) {
	a.wsMu.Lock()
	a.wsConn = conn
	a.wsServiceID = serviceID
	a.wsLastRxAt = time.Now()
	a.wsLastPingAt = time.Time{}
	a.wsProbeDeadline = time.Time{}
	a.wsMu.Unlock()
}

func (a *Adapter) closeWSConn() {
	a.wsMu.Lock()
	conn := a.wsConn
	a.wsConn = nil
	a.wsServiceID = 0
	a.wsLastRxAt = time.Time{}
	a.wsLastPingAt = time.Time{}
	a.wsProbeDeadline = time.Time{}
	a.wsMu.Unlock()
	if conn != nil {
		_ = wsCloseConn(conn)
	}
}

func (a *Adapter) writeWSPing() error {
	frame := larkws.NewPingFrame(a.currentWSServiceID())
	bs, err := frame.Marshal()
	if err != nil {
		return fmt.Errorf("marshal ping frame: %w", err)
	}
	if err := a.writeWSMessage(gws.BinaryMessage, bs); err != nil {
		return fmt.Errorf("write ping frame: %w", err)
	}
	a.noteWSPingSent(time.Now())
	return nil
}

func (a *Adapter) writeWSMessage(messageType int, data []byte) error {
	a.wsWriteMu.Lock()
	defer a.wsWriteMu.Unlock()

	a.wsMu.Lock()
	conn := a.wsConn
	a.wsMu.Unlock()
	if conn == nil {
		return fmt.Errorf("websocket connection is closed")
	}
	if err := wsSetWriteDeadline(conn, time.Now().Add(a.currentWSWriteTimeout())); err != nil {
		return fmt.Errorf("set websocket write deadline: %w", err)
	}
	return wsWriteFrame(conn, messageType, data)
}

func (a *Adapter) noteOutboundTransportFailure(err error) {
	if !shouldReconnectTransport(err) {
		return
	}
	a.wsMu.Lock()
	if !a.wsRecycleAt.IsZero() && time.Since(a.wsRecycleAt) < wsRecycleCooldown {
		a.wsMu.Unlock()
		return
	}
	a.wsRecycleAt = time.Now()
	conn := a.wsConn
	a.wsConn = nil
	a.wsServiceID = 0
	a.wsLastRxAt = time.Time{}
	a.wsLastPingAt = time.Time{}
	a.wsProbeDeadline = time.Time{}
	a.wsMu.Unlock()
	if conn != nil {
		slog.Warn("feishu outbound transport failed, recycling websocket", "error", err)
		_ = wsCloseConn(conn)
	}
}

func shouldReconnectTransport(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"network is unreachable",
		"no route to host",
		"eof",
		"use of closed network connection",
		"connection refused",
		"tls handshake timeout",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (a *Adapter) failWSLoop(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
	a.closeWSConn()
}

func (a *Adapter) noteWSPingSent(now time.Time) {
	a.wsMu.Lock()
	a.wsLastPingAt = now
	a.wsMu.Unlock()
}

func (a *Adapter) noteWSInboundActivity(now time.Time) {
	a.wsMu.Lock()
	a.wsLastRxAt = now
	a.wsProbeDeadline = time.Time{}
	a.wsMu.Unlock()
}

func (a *Adapter) currentWSLivenessSnapshot() (lastRxAt, lastPingAt, probeDeadline time.Time) {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	return a.wsLastRxAt, a.wsLastPingAt, a.wsProbeDeadline
}

func (a *Adapter) armWSProbe(now time.Time) bool {
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	if a.wsConn == nil {
		return false
	}
	if !a.wsProbeDeadline.IsZero() && now.Before(a.wsProbeDeadline) {
		return false
	}
	a.wsProbeDeadline = now.Add(wsProbeTimeout)
	return true
}

func (a *Adapter) startWSProbe(now time.Time, errCh chan<- error, reason string) bool {
	if !a.armWSProbe(now) {
		return true
	}
	if err := a.writeWSPing(); err != nil {
		a.failWSLoop(errCh, fmt.Errorf("%s: %w", reason, err))
		return false
	}
	return true
}

func (a *Adapter) wsLivenessMonitor(ctx context.Context, errCh chan<- error) {
	ticker := time.NewTicker(wsMonitorTick)
	defer ticker.Stop()
	lastWall := wsWallNow()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickNow := time.Now()
			pingInterval := a.currentWSPingInterval()
			wallNow := wsWallNow()
			switch wsWallClockAction(wallNow.Sub(lastWall), pingInterval) {
			case wsLivenessActionReconnect:
				a.failWSLoop(errCh, fmt.Errorf("wall clock jumped by %v after missing at least two heartbeat windows", wallNow.Sub(lastWall)))
				return
			case wsLivenessActionProbe:
				if ok := a.startWSProbe(tickNow, errCh, fmt.Sprintf("wall clock jumped by %v after missing one heartbeat window", wallNow.Sub(lastWall))); !ok {
					return
				}
			}
			lastWall = wallNow

			lastRxAt, lastPingAt, probeDeadline := a.currentWSLivenessSnapshot()
			if wsProbeTimedOut(tickNow, probeDeadline) {
				a.failWSLoop(errCh, fmt.Errorf("feishu websocket probe timed out after %v without inbound traffic", wsProbeTimeout))
				return
			}
			if wsShouldStartProbe(tickNow, lastPingAt, lastRxAt, probeDeadline, wsPongGrace) {
				if ok := a.startWSProbe(tickNow, errCh, fmt.Sprintf("no inbound websocket traffic within %v after ping", wsPongGrace)); !ok {
					return
				}
			}
		}
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = wsDefaultReconnectInterval
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func wsShouldStartProbe(now, lastPingAt, lastRxAt, probeDeadline time.Time, grace time.Duration) bool {
	if !probeDeadline.IsZero() {
		return false
	}
	return wsHeartbeatOverdue(now, lastPingAt, lastRxAt, grace)
}

func wsHeartbeatOverdue(now, lastPingAt, lastRxAt time.Time, grace time.Duration) bool {
	if grace <= 0 {
		grace = wsPongGrace
	}
	if lastPingAt.IsZero() {
		return false
	}
	if !lastRxAt.IsZero() && !lastRxAt.Before(lastPingAt) {
		return false
	}
	return !now.Before(lastPingAt.Add(grace))
}

func wsProbeTimedOut(now, probeDeadline time.Time) bool {
	if probeDeadline.IsZero() {
		return false
	}
	return !now.Before(probeDeadline)
}

func wsWallClockAction(gap, pingInterval time.Duration) wsLivenessAction {
	if pingInterval <= 0 {
		pingInterval = wsDefaultPingInterval
	}
	if gap >= pingInterval*2 {
		return wsLivenessActionReconnect
	}
	if gap >= pingInterval {
		return wsLivenessActionProbe
	}
	return wsLivenessActionNone
}

func wsReconnectDelayForExit(hadSession bool, configured time.Duration) time.Duration {
	if hadSession {
		return 0
	}
	if configured <= 0 {
		configured = wsDefaultReconnectInterval
	}
	if configured > wsMaxReconnectInterval {
		return wsMaxReconnectInterval
	}
	return configured
}
