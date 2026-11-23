package store

import (
	"testing"
	"time"
)

func TestWakeupsSignalOnlyMatchingWaiters(t *testing.T) {
	var w wakeups
	first, stopFirst := w.subscribe("queue:a")
	second, stopSecond := w.subscribe("queue:a")
	other, stopOther := w.subscribe("queue:b")
	defer stopOther()
	w.signal("queue:a")
	w.signal("queue:a") // coalesces; must not block
	for name, ch := range map[string]<-chan struct{}{"first": first, "second": second} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("%s waiter was not woken", name)
		}
	}
	select {
	case <-other:
		t.Fatal("waiter for another key was woken")
	default:
	}
	stopFirst()
	stopSecond()
	if len(w.waiters) != 1 {
		t.Fatalf("stopped waiters must be removed: %v", w.waiters)
	}
	w.signal("queue:a") // no waiters left; must not panic
}
