package runtime

import (
	"sync"
	"testing"
)

func TestSubmissionStartsSerializesAndRetries(t *testing.T) {
	var guard SubmissionStarts
	const callers = 32
	var wg sync.WaitGroup
	results := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- guard.TryBegin("same") }()
	}
	wg.Wait()
	close(results)
	admitted := 0
	for result := range results {
		if result {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("admitted %d concurrent starts", admitted)
	}
	if !guard.TryBegin("other") {
		t.Fatal("other session blocked")
	}
	if !guard.Finish("same") || guard.Finish("same") {
		t.Fatal("concurrent arrivals must leave exactly one retry")
	}
	if !guard.TryBegin("same") || guard.Finish("same") {
		t.Fatal("clean retry must not request further work")
	}
	guard.Finish("other")
}
