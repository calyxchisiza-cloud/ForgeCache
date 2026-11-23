package forgecache

import (
	"sync"
	"testing"
	"time"
)

func TestStatsCountHitsMissesEvictionsAndExpirations(t *testing.T) {
	clock := &manualClock{now: time.Unix(3_000, 0)}
	c := New(WithJanitor(0), WithClock(clock))
	defer c.Close()
	if _, found := c.Get("missing"); found {
		t.Fatal("missing key was stored")
	}
	if err := c.Set("A", []byte("a"), time.Second); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("A"); !found {
		t.Fatal("A missing")
	}
	if _, found := c.Get("A"); !found {
		t.Fatal("A missing on the second read")
	}
	clock.Set(clock.Now().Add(time.Second))
	if _, found := c.Get("A"); found {
		t.Fatal("expired A was returned")
	}
	st := c.Stats()
	if st.Hits != 2 || st.Misses != 2 || st.Evictions != 0 || st.Expirations != 1 {
		t.Fatalf("stats %+v", st)
	}
	if st.HitRatio < 0.49 || st.HitRatio > 0.51 {
		t.Fatalf("ratio %v", st.HitRatio)
	}
	if st.Keys != 0 || st.OpsPerSec <= 0 || st.P99 < st.P95 {
		t.Fatalf("stats %+v", st)
	}

	limited := New(WithJanitor(0), WithMaxBytes(2))
	defer limited.Close()
	if err := limited.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	if err := limited.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	st = limited.Stats()
	if st.Evictions != 1 || st.Keys != 1 || st.Bytes != 2 {
		t.Fatalf("eviction stats %+v", st)
	}
}

func TestGetOrLoadRunsOnce(t *testing.T) {
	c := New(WithJanitor(0))
	defer c.Close()
	const n = 8
	release := make(chan struct{})
	started := make(chan struct{})
	var ready sync.Once
	var calls int
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			val, err := c.GetOrLoad("user:15", time.Minute, func() ([]byte, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				ready.Do(func() { close(started) })
				<-release
				return []byte("Ada"), nil
			})
			if err != nil {
				errCh <- err
				return
			}
			if string(val) != "Ada" {
				errCh <- errValue(val)
			}
		}()
	}
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		f := c.inflight["user:15"]
		waiting := 0
		if f != nil {
			waiting = f.waiters
		}
		c.mu.Unlock()
		if waiting >= n-1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiters %d", waiting)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("load calls %d", gotCalls)
	}
	st := c.Stats()
	if st.Misses != 1 || st.Hits != n-1 {
		t.Fatalf("stats %+v", st)
	}
	if _, found := c.Get("user:15"); !found {
		t.Fatal("loaded value was not stored")
	}
}

type errValue []byte

func (e errValue) Error() string { return "unexpected value " + string(e) }

func TestShardsKeepSeparateEntries(t *testing.T) {
	const shards = 4
	s := NewSharded(shards, WithJanitor(0), WithMaxBytes(shards*2))
	defer s.Close()
	keys := make([]string, shards)
	for shard := 0; shard < shards; shard++ {
		for i := 0; i < 256; i++ {
			key := string([]byte{byte(i)})
			if shardIndex(key, shards) == shard {
				keys[shard] = key
				break
			}
		}
		if keys[shard] == "" {
			t.Fatalf("no key for shard %d", shard)
		}
		if err := s.Set(keys[shard], []byte("x"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != shards || s.Bytes() != shards*2 {
		t.Fatalf("len %d bytes %d", s.Len(), s.Bytes())
	}
	extra := ""
	for i := 0; i < 256; i++ {
		key := string([]byte{byte(i)})
		if shardIndex(key, shards) == 0 && key != keys[0] {
			extra = key
			break
		}
	}
	if err := s.Set(extra, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := s.Get(keys[1]); !found {
		t.Fatal("a full shard evicted a key from another shard")
	}
	var wg sync.WaitGroup
	wg.Add(4)
	for g := 0; g < 4; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := "n" + itoa(g*50+i)
				_ = s.Set(key, []byte("v"), 0)
				s.Get(key)
			}
		}(g)
	}
	wg.Wait()
	if s.Stats().OpsPerSec <= 0 {
		t.Fatal("sharded stats recorded no operations")
	}
}
