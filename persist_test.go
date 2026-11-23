package forgecache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreSetDeleteAndClear(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("name", []byte("Calyx"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("bin", []byte{0, 1, 255}, 0); err != nil {
		t.Fatal(err)
	}
	deleted, err := c.Delete("name")
	if err != nil || !deleted {
		t.Fatalf("delete found %v err %v", deleted, err)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("bin", []byte{0, 1, 255}, 0); err != nil {
		t.Fatal(err)
	}
	c.Close()

	c, err = Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, found := c.Get("name"); found {
		t.Fatal("deleted name was restored")
	}
	got, found := c.Get("bin")
	if !found || len(got) != 3 || got[0] != 0 || got[2] != 255 {
		t.Fatalf("bin %v found %v", got, found)
	}
}

func TestRestoreDiscardsExpiredEntries(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("temp", []byte("x"), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("keep", []byte("y"), 0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	clock.Set(clock.Now().Add(10 * time.Second))
	c, err = Open(dir, WithJanitor(0), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, found := c.Get("temp"); found {
		t.Fatal("expired value was restored")
	}
	if _, found := c.Get("keep"); !found {
		t.Fatal("live value was dropped")
	}
}

func TestExpireIsRestored(t *testing.T) {
	clock := &manualClock{now: time.Unix(2_000, 0)}
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("code", []byte("837491"), 0); err != nil {
		t.Fatal(err)
	}
	found, err := c.Expire("code", time.Hour)
	if err != nil || !found {
		t.Fatalf("expire found %v err %v", found, err)
	}
	c.Close()
	c, err = Open(dir, WithJanitor(0), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	remain, ok := c.TTL("code")
	if !ok || remain != time.Hour {
		t.Fatalf("ttl %s ok %v", remain, ok)
	}
}

func TestSnapshotThenReplay(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	deleted, err := c.Delete("A")
	if err != nil || !deleted {
		t.Fatalf("delete found %v err %v", deleted, err)
	}
	c.Close()

	c, err = Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, found := c.Get("A"); found {
		t.Fatal("deleted key survived the snapshot")
	}
	got, found := c.Get("B")
	if !found || string(got) != "b" {
		t.Fatalf("got %q found %v", got, found)
	}
}

func TestIncompleteFinalRecordIsIgnored(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	path := filepath.Join(dir, aofName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-3); err != nil {
		t.Fatal(err)
	}
	c, err = Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, found := c.Get("A"); !found {
		t.Fatal("complete record was dropped")
	}
	if _, found := c.Get("B"); found {
		t.Fatal("torn record was applied")
	}
}

func TestBadChecksumStopsRecovery(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	path := filepath.Join(dir, aofName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, WithJanitor(0)); !errors.Is(err, ErrCorruptLog) {
		t.Fatalf("error %v", err)
	}
}

func TestReplayEvictsAndDropsAccessOrder(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0), WithMaxBytes(4))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"A", "B"} {
		if err := c.Set(key, []byte("x"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, found := c.Get("A"); !found {
		t.Fatal("A missing before the write that evicts")
	}
	if err := c.Set("C", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("B"); found {
		t.Fatal("B should have been evicted in memory")
	}
	c.Close()

	c, err = Open(dir, WithJanitor(0), WithMaxBytes(4))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, found := c.Get("A"); found {
		t.Fatal("restart kept the access order")
	}
	if _, found := c.Get("B"); !found {
		t.Fatal("replay dropped B")
	}
	if _, found := c.Get("C"); !found || c.Len() != 2 {
		t.Fatalf("len %d", c.Len())
	}
}

func TestDurabilityOSRestoresAfterClose(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, WithJanitor(0), WithDurability(DurabilityOS))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("name", []byte("Calyx"), 0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c, err = Open(dir, WithJanitor(0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, found := c.Get("name")
	if !found || string(got) != "Calyx" {
		t.Fatalf("got %q found %v", got, found)
	}
}

func TestSnapshotRequiresADirectory(t *testing.T) {
	c := New(WithJanitor(0))
	defer c.Close()
	if err := c.Snapshot(); !errors.Is(err, ErrNoPersistence) {
		t.Fatalf("error %v", err)
	}
}
