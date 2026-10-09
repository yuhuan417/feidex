package claudecli

import (
	"testing"
	"time"
)

// TestToolProgressFramesAreDroppedSilently pins the treatment of the CLI's
// long-running-tool heartbeat: it repeats the parent tool_use_id with an
// -heartbeat-N suffix and an elapsed count, carries nothing the bridge acts on,
// and must not be reported as a protocol error once per heartbeat.
func TestToolProgressFramesAreDroppedSilently(t *testing.T) {
	frame := []byte(`{"type":"tool_progress","tool_use_id":"call_00_x-heartbeat-0",` +
		`"tool_name":"Bash","parent_tool_use_id":"call_00_x","elapsed_time_seconds":30,` +
		`"heartbeat":true,"session_id":"s","uuid":"u"}`)
	msg, err := parseWireMessage(frame)
	if err != nil {
		t.Fatalf("parseWireMessage(tool_progress) error = %v, want it dropped", err)
	}
	if msg != nil {
		t.Fatalf("parseWireMessage(tool_progress) = %#v, want nil (nothing consumes it)", msg)
	}

	// A genuinely unknown top-level type must still fail loudly.
	if _, err := parseWireMessage([]byte(`{"type":"brand_new_thing"}`)); err == nil {
		t.Fatal("unknown message type should still be an error")
	}
}

// TestSessionStaysSilentOnToolProgress covers the session half: the heartbeat
// must not surface as a session event at all, since the bridge used to log a
// protocol error for every one of them.
func TestSessionStaysSilentOnToolProgress(t *testing.T) {
	session := NewSession()
	session.handleLine([]byte(`{"type":"tool_progress","tool_use_id":"call_00_x-heartbeat-0",` +
		`"tool_name":"Bash","parent_tool_use_id":"call_00_x","elapsed_time_seconds":30,` +
		`"heartbeat":true,"session_id":"s","uuid":"u"}`))

	select {
	case event := <-session.Events():
		t.Fatalf("tool_progress emitted %#v, want nothing", event)
	case <-time.After(50 * time.Millisecond):
	}
}
