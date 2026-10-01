package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeReplier struct {
	got  []string
	fail int // index to fail at, -1 for none
}

func (f *fakeReplier) ReplyTextWithID(_ context.Context, _, text string, _ bool) (string, error) {
	if f.fail >= 0 && len(f.got) == f.fail {
		return "", errors.New("send failed")
	}
	f.got = append(f.got, text)
	return "om_" + string(rune('0'+len(f.got))), nil
}

func TestReplyTextChunkedSplitsLongBody(t *testing.T) {
	long := strings.Repeat("line of text\n", 3000) // ~39KB, over the 20KB budget
	f := &fakeReplier{fail: -1}
	id, err := replyTextChunked(context.Background(), f, "om_x", long, false)
	if err != nil {
		t.Fatalf("replyTextChunked() error = %v", err)
	}
	if id == "" {
		t.Fatal("expected a message id")
	}
	if len(f.got) < 2 {
		t.Fatalf("chunks = %d, want more than one for a %d-byte body", len(f.got), len(long))
	}
	if joined := strings.Join(f.got, ""); len(joined) < len(long)/2 {
		t.Fatalf("chunks lost content: %d of %d bytes", len(joined), len(long))
	}
}

func TestReplyTextChunkedSendsShortBodyVerbatim(t *testing.T) {
	f := &fakeReplier{fail: -1}
	if _, err := replyTextChunked(context.Background(), f, "om_x", "short body", false); err != nil {
		t.Fatalf("replyTextChunked() error = %v", err)
	}
	if len(f.got) != 1 || f.got[0] != "short body" {
		t.Fatalf("got %q, want the body sent once unchanged", f.got)
	}
}

func TestReplyTextChunkedPropagatesFirstChunkFailure(t *testing.T) {
	f := &fakeReplier{fail: 0}
	if _, err := replyTextChunked(context.Background(), f, "om_x", strings.Repeat("x\n", 20000), false); err == nil {
		t.Fatal("expected the first chunk failure to propagate")
	}
}

func TestReplyTextChunkedReportsPartialDelivery(t *testing.T) {
	f := &fakeReplier{fail: 1}
	id, err := replyTextChunked(context.Background(), f, "om_x", strings.Repeat("x\n", 20000), false)
	if err != nil {
		t.Fatalf("partial delivery should not surface as an error, got %v", err)
	}
	if id != "om_1" {
		t.Fatalf("id = %q, want the first delivered message id", id)
	}
}
