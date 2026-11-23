package forgecache

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConcurrentOperationsKeepTheListIntact(t *testing.T) {
	c := New(WithMaxBytes(256), WithJanitor(5*time.Millisecond))
	defer c.Close()
	const workers = 8
	const ops = 400
	var wg sync.WaitGroup
	wg.Add(workers)
	for id := 0; id < workers; id++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := "k" + itoa((id*17+i)%48)
				switch i % 6 {
				case 0:
					_ = c.Set(key, []byte(itoa(i)), 0)
				case 1:
					c.Get(key)
				case 2:
					c.Delete(key)
				case 3:
					_, _ = c.Expire(key, time.Millisecond)
				case 4:
					_ = c.Exists(key)
				default:
					_, _ = c.TTL(key)
					c.Sweep()
				}
			}
		}(id)
	}
	wg.Wait()
	if err := c.checkInvariants(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("final", []byte("ok"), 0); err != nil {
		t.Fatal(err)
	}
	got, found := c.Get("final")
	if !found || string(got) != "ok" {
		t.Fatalf("got %q found %v", got, found)
	}
	if err := c.checkInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUpdateOfOneKey(t *testing.T) {
	c := New(WithJanitor(0))
	defer c.Close()
	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for id := 0; id < workers; id++ {
		go func(id int) {
			defer wg.Done()
			value := []byte(itoa(id))
			for i := 0; i < 200; i++ {
				if err := c.Set("one", value, 0); err != nil {
					t.Errorf("set: %v", err)
					return
				}
				c.Get("one")
				if i%5 == 0 {
					c.Delete("one")
				}
			}
		}(id)
	}
	wg.Wait()
	if err := c.checkInvariants(); err != nil {
		t.Fatal(err)
	}
}

func (c *Cache) checkInvariants() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) == 0 {
		if c.head != nil || c.tail != nil || c.bytes != 0 {
			return errors.New("empty cache still has links or bytes")
		}
		return nil
	}
	seen := make(map[*entry]bool, len(c.items))
	bytes := 0
	var last *entry
	for e := c.head; e != nil; e = e.next {
		if seen[e] {
			return errors.New("recency list contains a cycle")
		}
		seen[e] = true
		if c.items[e.key] != e {
			return errors.New("list entry is missing from the map")
		}
		if e.prev == nil {
			if c.head != e {
				return errors.New("head link is wrong")
			}
		} else if e.prev.next != e {
			return errors.New("prev link is wrong")
		}
		bytes += entrySize(e.key, e.value)
		last = e
		if len(seen) > len(c.items) {
			return errors.New("list is longer than the map")
		}
	}
	if len(seen) != len(c.items) {
		return errors.New("map contains an entry the list does not")
	}
	if last != c.tail {
		return errors.New("tail is not the least recently used entry")
	}
	if bytes != c.bytes {
		return errors.New("byte count does not match the stored entries")
	}
	return nil
}
