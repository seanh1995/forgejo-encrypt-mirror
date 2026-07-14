package webhook

import (
	"testing"
	"time"
)

func TestReplayCacheDetectsDuplicate(t *testing.T) {
	c := NewReplayCache(time.Hour)

	if c.CheckAndRemember("delivery-1") {
		t.Fatal("first sighting of delivery-1 should not be a replay")
	}
	if !c.CheckAndRemember("delivery-1") {
		t.Fatal("second sighting of delivery-1 should be detected as a replay")
	}
}

func TestReplayCacheDistinctIDs(t *testing.T) {
	c := NewReplayCache(time.Hour)

	if c.CheckAndRemember("a") {
		t.Fatal("unexpected replay for a")
	}
	if c.CheckAndRemember("b") {
		t.Fatal("unexpected replay for b")
	}
}

func TestReplayCacheEmptyIDNeverReplay(t *testing.T) {
	c := NewReplayCache(time.Hour)

	if c.CheckAndRemember("") {
		t.Fatal("empty id should never be treated as a replay")
	}
	if c.CheckAndRemember("") {
		t.Fatal("empty id should never be treated as a replay")
	}
}

func TestReplayCacheNilSafe(t *testing.T) {
	var c *ReplayCache
	if c.CheckAndRemember("x") {
		t.Fatal("nil cache should never report a replay")
	}
}

func TestReplayCacheExpiry(t *testing.T) {
	c := NewReplayCache(10 * time.Millisecond)

	if c.CheckAndRemember("delivery-1") {
		t.Fatal("first sighting should not be a replay")
	}

	time.Sleep(30 * time.Millisecond)

	if c.CheckAndRemember("delivery-1") {
		t.Fatal("expected entry to have expired and not be treated as a replay")
	}
}
