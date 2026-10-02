package runtime

import (
	"sync"
	"testing"
)

func TestSessionActorsSerializeSameKeyAndAllowDifferentKeys(t *testing.T) {
	actors := NewSessionActors()
	var mu sync.Mutex
	active := 0
	maxActive := 0
	start := make(chan struct{})
	entered := make(chan struct{}, 3)
	finish := make(chan struct{})
	run := func(key string) {
		actors.Run(key, func() {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			entered <- struct{}{}
			<-start
			mu.Lock()
			active--
			mu.Unlock()
			finish <- struct{}{}
		})
	}
	go run("same")
	go run("same")
	go run("other")
	<-entered
	<-entered
	mu.Lock()
	if maxActive != 2 {
		t.Fatalf("max active = %d, want different keys to run concurrently", maxActive)
	}
	mu.Unlock()
	close(start)
	<-finish
	<-finish
	<-finish
}
