package engine

import (
	"strings"
	"sync"
	"testing"
)

func TestQueueRunsInteractiveBeforeBackground(t *testing.T) {
	q := NewQueue()
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	started := make(chan struct{})
	q.Add(Background, func() { close(started); <-release; mu.Lock(); order = append(order, "first"); mu.Unlock() })
	<-started
	for _, name := range []string{"bg1", "bg2"} {
		q.Add(Background, func() { mu.Lock(); order = append(order, name); mu.Unlock() })
	}
	q.Add(Interactive, func() { mu.Lock(); order = append(order, "mine"); mu.Unlock() })
	close(release)
	q.Wait()
	if got := strings.Join(order, ","); got != "first,mine,bg1,bg2" {
		t.Fatalf("order %s", got)
	}
}
