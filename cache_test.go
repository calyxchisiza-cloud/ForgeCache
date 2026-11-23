package forgecache

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestSetGetUpdateAndDelete(t *testing.T) {
	c := New()
	if _, found := c.Get("name"); found {
		t.Fatal("missing key was found")
	}
	if err := c.Set("name", []byte("Calyx"), 0); err != nil {
		t.Fatal(err)
	}
	got, found := c.Get("name")
	if !found || string(got) != "Calyx" {
		t.Fatalf("got %q found %v", got, found)
	}
	if err := c.Set("name", []byte("Ada"), 0); err != nil {
		t.Fatal(err)
	}
	got, found = c.Get("name")
	if !found || string(got) != "Ada" {
		t.Fatalf("updated got %q found %v", got, found)
	}
	if !c.Delete("name") {
		t.Fatal("delete did not find the key")
	}
	if _, found := c.Get("name"); found {
		t.Fatal("deleted key was found")
	}
	if c.Delete("name") {
		t.Fatal("second delete reported a hit")
	}
}

func TestValueCopiesDoNotAlias(t *testing.T) {
	c := New()
	in := []byte("Calyx")
	if err := c.Set("name", in, 0); err != nil {
		t.Fatal(err)
	}
	in[0] = 'X'
	got, found := c.Get("name")
	if !found || string(got) != "Calyx" {
		t.Fatalf("stored value changed with the caller buffer: %q", got)
	}
	got[0] = 'Z'
	again, found := c.Get("name")
	if !found || string(again) != "Calyx" {
		t.Fatalf("returned buffer aliases the cache: %q", again)
	}
}

func TestEmptyAndBinaryValues(t *testing.T) {
	c := New()
	if err := c.Set("empty", nil, 0); err != nil {
		t.Fatal(err)
	}
	got, found := c.Get("empty")
	if !found || got == nil || len(got) != 0 {
		t.Fatalf("empty value %v found %v", got, found)
	}
	raw := []byte{0, 1, 2, 255}
	if err := c.Set("bin", raw, 0); err != nil {
		t.Fatal(err)
	}
	got, found = c.Get("bin")
	if !found || !bytes.Equal(got, raw) {
		t.Fatalf("binary got %v", got)
	}
}

func TestKeyAndTTLValidation(t *testing.T) {
	c := New()
	if err := c.Set("", []byte("x"), 0); !errors.Is(err, ErrEmptyKey) {
		t.Fatalf("empty key error %v", err)
	}
	long := string(bytes.Repeat([]byte("a"), MaxKeyBytes+1))
	if err := c.Set(long, []byte("x"), 0); !errors.Is(err, ErrKeyTooLong) {
		t.Fatalf("long key error %v", err)
	}
	if _, found := c.Get(long); found {
		t.Fatal("rejected key was stored")
	}
	edge := string(bytes.Repeat([]byte("b"), MaxKeyBytes))
	if err := c.Set(edge, []byte("ok"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("name", []byte("x"), -time.Second); !errors.Is(err, ErrNegativeTTL) {
		t.Fatalf("negative ttl error %v", err)
	}
	if err := c.Set("name", []byte("x"), time.Minute); !errors.Is(err, ErrTTLUnsupported) {
		t.Fatalf("positive ttl error %v", err)
	}
	if _, found := c.Get("name"); found {
		t.Fatal("rejected ttl stored a value")
	}
}

func TestManyKeys(t *testing.T) {
	c := New()
	const n = 10000
	for i := 0; i < n; i++ {
		key := "user:" + itoa(i)
		if err := c.Set(key, []byte(itoa(i)), 0); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		got, found := c.Get("user:" + itoa(i))
		if !found || string(got) != itoa(i) {
			t.Fatalf("key %d got %q found %v", i, got, found)
		}
	}
	if _, found := c.Get("user:missing"); found {
		t.Fatal("unexpected hit")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
