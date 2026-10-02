package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type groupTestFrontend struct {
	name                          string
	events                        *[]string
	prepareErr, serveErr, stopErr error
}

func (f *groupTestFrontend) note(phase string)             { *f.events = append(*f.events, f.name+":"+phase) }
func (f *groupTestFrontend) Prepare(context.Context) error { f.note("prepare"); return f.prepareErr }
func (f *groupTestFrontend) StartInboundGC()               { f.note("gc") }
func (f *groupTestFrontend) RecoverShared()                { f.note("shared") }
func (f *groupTestFrontend) RecoverFrontend()              { f.note("recover") }
func (f *groupTestFrontend) Serve() error                  { f.note("serve"); return f.serveErr }
func (f *groupTestFrontend) StartBackground()              { f.note("background") }
func (f *groupTestFrontend) Stop(context.Context) error    { f.note("stop"); return f.stopErr }
func TestFrontendGroupRecoversBeforeServingAndStopsInReverse(t *testing.T) {
	var events []string
	a, b := &groupTestFrontend{name: "a", events: &events}, &groupTestFrontend{name: "b", events: &events}
	g := FrontendGroup{Frontends: []ManagedFrontend{a, b}}
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("b shutdown")
	b.stopErr = failure
	if err := g.Stop(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("stop error = %v", err)
	}
	want := []string{"a:prepare", "b:prepare", "a:gc", "b:gc", "a:shared", "a:recover", "b:recover", "a:serve", "b:serve", "a:background", "b:background", "b:stop", "a:stop"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("phases = %v", events)
	}
}
func TestFrontendGroupRollsBackIncludingPartiallyPreparedFrontend(t *testing.T) {
	failure := errors.New("prepare failed")
	var events []string
	a, b, c := &groupTestFrontend{name: "a", events: &events}, &groupTestFrontend{name: "b", events: &events, prepareErr: failure}, &groupTestFrontend{name: "c", events: &events}
	if err := (FrontendGroup{Frontends: []ManagedFrontend{a, b, c}}).Start(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("start error = %v", err)
	}
	if want := []string{"a:prepare", "b:prepare", "b:stop", "a:stop"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("rollback phases = %v", events)
	}
}
