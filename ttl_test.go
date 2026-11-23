package forgecache

import (
	"sync"
	"testing"
	"time"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (m *manualClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *manualClock) Set(t time.Time) {
	m.mu.Lock()
	m.now = t
	m.mu.Unlock()
}

func TestExpiredValueIsAMissBeforeSweep(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	c := New(WithClock(clock), WithJanitor(0))
	defer c.Close()
	if err := c.Set("code", []byte("837491"), 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if !c.Exists("code") {
		t.Fatal("fresh key was missing")
	}
	remain, ok := c.TTL("code")
	if !ok || remain != 60*time.Second {
		t.Fatalf("ttl %s ok %v", remain, ok)
	}
	clock.Set(clock.Now().Add(60 * time.Second))
	if _, found := c.Get("code"); found {
		t.Fatal("expired value was returned")
	}
	if c.Exists("code") || c.Len() != 0 {
		t.Fatalf("expired key still stored, len %d", c.Len())
	}
	if _, ok := c.TTL("code"); ok {
		t.Fatal("expired key still has a ttl")
	}
}

func TestZeroTTLDoesNotExpire(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	c := New(WithClock(clock), WithJanitor(0))
	defer c.Close()
	if err := c.Set("name", []byte("Calyx"), 0); err != nil {
		t.Fatal(err)
	}
	remain, ok := c.TTL("name")
	if !ok || remain != -1 {
		t.Fatalf("ttl %s ok %v", remain, ok)
	}
	clock.Set(clock.Now().Add(24 * time.Hour))
	got, found := c.Get("name")
	if !found || string(got) != "Calyx" {
		t.Fatalf("got %q found %v", got, found)
	}
}

func TestExpireReplacesDeadline(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	c := New(WithClock(clock), WithJanitor(0))
	defer c.Close()
	if err := c.Set("code", []byte("837491"), time.Minute); err != nil {
		t.Fatal(err)
	}
	found, err := c.Expire("code", 5*time.Second)
	if err != nil || !found {
		t.Fatalf("expire found %v err %v", found, err)
	}
	clock.Set(clock.Now().Add(5 * time.Second))
	if _, found := c.Get("code"); found {
		t.Fatal("value lived past the replaced deadline")
	}
	found, err = c.Expire("missing", time.Second)
	if err != nil || found {
		t.Fatalf("missing expire found %v err %v", found, err)
	}
	if _, err := c.Expire("code", -time.Second); err != ErrNegativeTTL {
		t.Fatalf("negative expire err %v", err)
	}
}

func TestSweepRemovesExpiredWithoutARead(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	c := New(WithClock(clock), WithJanitor(0), WithMaxBytes(4))
	defer c.Close()
	if err := c.Set("A", []byte("a"), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(time.Second))
	if n := c.Sweep(); n != 1 {
		t.Fatalf("swept %d", n)
	}
	if _, found := c.Get("A"); found {
		t.Fatal("swept key was returned")
	}
	if _, found := c.Get("B"); !found || c.Bytes() != 2 {
		t.Fatalf("bytes %d", c.Bytes())
	}
}

func TestJanitorRemovesExpiredEntry(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	c := New(WithClock(clock), WithJanitor(20*time.Millisecond))
	defer c.Close()
	if err := c.Set("code", []byte("837491"), time.Minute); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(time.Minute))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Len() == 0 {
			if _, found := c.Get("code"); found {
				t.Fatal("janitor left a readable value")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("janitor did not remove the expired key")
}
