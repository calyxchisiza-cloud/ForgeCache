package forgecache

import (
	"errors"
	"testing"
)

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	// Each of A, B, C, D is 1 key byte + 1 value byte.
	c := New(WithMaxBytes(6))
	for _, key := range []string{"A", "B", "C"} {
		if err := c.Set(key, []byte(key), 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, found := c.Get("A"); !found {
		t.Fatal("A missing before eviction")
	}
	if err := c.Set("D", []byte("D"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("B"); found {
		t.Fatal("B was not evicted")
	}
	for _, key := range []string{"A", "C", "D"} {
		got, found := c.Get(key)
		if !found || string(got) != key {
			t.Fatalf("key %s got %q found %v", key, got, found)
		}
	}
	if c.Len() != 3 || c.Bytes() != 6 {
		t.Fatalf("len %d bytes %d", c.Len(), c.Bytes())
	}
}

func TestOldestInsertIsEvictedWhenNothingIsReread(t *testing.T) {
	c := New(WithMaxBytes(6))
	for _, key := range []string{"A", "B", "C"} {
		if err := c.Set(key, []byte("x"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Set("D", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("A"); found {
		t.Fatal("oldest key survived")
	}
	if c.Len() != 3 {
		t.Fatalf("len %d", c.Len())
	}
}

func TestUpdateMovesEntryAndCanEvictOthers(t *testing.T) {
	c := New(WithMaxBytes(8))
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	// A grows from 2 bytes to 7. B is 2. Together they no longer fit in 8.
	if err := c.Set("A", []byte("aaaaaa"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("B"); found {
		t.Fatal("B was not evicted to make room for the update")
	}
	got, found := c.Get("A")
	if !found || string(got) != "aaaaaa" || c.Bytes() != 7 {
		t.Fatalf("got %q bytes %d found %v", got, c.Bytes(), found)
	}
}

func TestEntryLargerThanCapacityIsRejected(t *testing.T) {
	c := New(WithMaxBytes(4))
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	err := c.Set("wide", []byte("too-big"), 0)
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("error %v", err)
	}
	got, found := c.Get("A")
	if !found || string(got) != "a" || c.Len() != 1 {
		t.Fatalf("cache changed after a rejected set: %q len %d", got, c.Len())
	}
	err = c.Set("A", []byte("too-big"), 0)
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("update error %v", err)
	}
	got, found = c.Get("A")
	if !found || string(got) != "a" {
		t.Fatalf("rejected update replaced A with %q", got)
	}
}

func TestDeleteFreesCapacity(t *testing.T) {
	c := New(WithMaxBytes(4))
	if err := c.Set("A", []byte("a"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("B", []byte("b"), 0); err != nil {
		t.Fatal(err)
	}
	deleted, err := c.Delete("A")
	if err != nil || !deleted {
		t.Fatalf("delete found %v err %v", deleted, err)
	}
	if err := c.Set("C", []byte("c"), 0); err != nil {
		t.Fatal(err)
	}
	if _, found := c.Get("B"); !found {
		t.Fatal("B was evicted after A was deleted")
	}
	if _, found := c.Get("C"); !found || c.Bytes() != 4 {
		t.Fatalf("len %d bytes %d", c.Len(), c.Bytes())
	}
}
