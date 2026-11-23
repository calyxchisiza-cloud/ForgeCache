package forgecache

import "testing"

func BenchmarkCacheGet(b *testing.B) {
	c := New(WithJanitor(0))
	defer c.Close()
	if err := c.Set("k", []byte("v"), 0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get("k")
	}
}

func BenchmarkMapGet(b *testing.B) {
	m := map[string][]byte{"k": []byte("v")}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m["k"]
	}
}

func BenchmarkCacheSetEvict(b *testing.B) {
	c := New(WithJanitor(0), WithMaxBytes(64))
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "k" + itoa(i%32)
		if err := c.Set(key, []byte("v"), 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShardedGet(b *testing.B) {
	s := NewSharded(8, WithJanitor(0))
	defer s.Close()
	if err := s.Set("k", []byte("v"), 0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Get("k")
	}
}
