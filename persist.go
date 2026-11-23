package forgecache

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	opSet    byte = 1
	opDelete byte = 2
	opClear  byte = 3

	aofName                = "append.log"
	snapshotName           = "snapshot.bin"
	snapshotOld            = "snapshot.bin.old"
	snapshotTmp            = "snapshot.bin.tmp"
	snapshotMagic          = "FCSP"
	snapshotVersion uint32 = 1

	// maxPersistValue bounds one logged value so a corrupt length cannot
	// allocate an unbounded buffer.
	maxPersistValue = 32 << 20
)

// Durability selects how often a logged mutation is flushed to disk.
type Durability int

const (
	// DurabilitySync flushes the log after every mutation.
	DurabilitySync Durability = iota
	// DurabilityOS leaves flushing to the operating system until Close or Snapshot.
	DurabilityOS
)

var errIncomplete = errors.New("incomplete cache record")

// WithDurability selects log flushing. The default is DurabilitySync.
func WithDurability(d Durability) Option {
	return func(c *Cache) {
		c.syncWrites = d == DurabilitySync
	}
}

// Open restores a cache from dir, then appends later mutations to its log.
// The directory is created when it is missing.
func Open(dir string, opts ...Option) (*Cache, error) {
	if dir == "" {
		return nil, ErrNoPersistence
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := New(opts...)
	c.dir = dir
	if err := c.load(); err != nil {
		c.Close()
		return nil, err
	}
	c.mu.Lock()
	err := c.reopenLogLocked()
	c.mu.Unlock()
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Snapshot writes every unexpired entry and discards log records that are
// already included. The replacement is a temp file, then a rename.
// Snapshot blocks other operations until the files are in place.
func (c *Cache) Snapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dir == "" {
		return ErrNoPersistence
	}
	body, err := c.snapshotBody()
	if err != nil {
		return err
	}
	tmp := filepath.Join(c.dir, snapshotTmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	final := filepath.Join(c.dir, snapshotName)
	old := filepath.Join(c.dir, snapshotOld)
	_ = os.Remove(old)
	if err := os.Rename(final, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	_ = os.Remove(old)
	return c.rotateLogLocked()
}

func (c *Cache) snapshotBody() ([]byte, error) {
	now := c.now()
	var entries []*entry
	for e := c.head; e != nil; e = e.next {
		if !c.expired(e, now) {
			entries = append(entries, e)
		}
	}
	var buf bytes.Buffer
	buf.WriteString(snapshotMagic)
	var num [4]byte
	binary.BigEndian.PutUint32(num[:], snapshotVersion)
	buf.Write(num[:])
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], c.seq)
	buf.Write(seq[:])
	binary.BigEndian.PutUint32(num[:], uint32(len(entries)))
	buf.Write(num[:])
	for _, e := range entries {
		buf.Write(counted(e.key))
		buf.Write(countedBytes(e.value))
		buf.Write(encodeExpires(e.expires))
	}
	sum := crc32.ChecksumIEEE(buf.Bytes())
	binary.BigEndian.PutUint32(num[:], sum)
	buf.Write(num[:])
	return buf.Bytes(), nil
}

func (c *Cache) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	seq, err := c.readSnapshot()
	if err != nil {
		return err
	}
	c.seq = seq
	return c.replay()
}

func (c *Cache) readSnapshot() (uint64, error) {
	path := filepath.Join(c.dir, snapshotName)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		path = filepath.Join(c.dir, snapshotOld)
		f, err = os.Open(path)
	}
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	if len(body) < len(snapshotMagic)+4+8+4+4 {
		return 0, ErrCorruptLog
	}
	payload := body[:len(body)-4]
	got := binary.BigEndian.Uint32(body[len(body)-4:])
	if crc32.ChecksumIEEE(payload) != got {
		return 0, ErrCorruptLog
	}
	if string(payload[:4]) != snapshotMagic {
		return 0, ErrCorruptLog
	}
	version := binary.BigEndian.Uint32(payload[4:8])
	if version != snapshotVersion {
		return 0, ErrCorruptLog
	}
	seq := binary.BigEndian.Uint64(payload[8:16])
	count := binary.BigEndian.Uint32(payload[16:20])
	rest := payload[20:]
	now := c.now()
	for i := uint32(0); i < count; i++ {
		key, rest2, err := takeCounted(rest, MaxKeyBytes)
		if err != nil {
			return 0, err
		}
		value, rest3, err := takeCounted(rest2, maxPersistValue)
		if err != nil {
			return 0, err
		}
		expires, rest4, err := takeExpires(rest3)
		if err != nil {
			return 0, err
		}
		rest = rest4
		if !expires.IsZero() && !now.Before(expires) {
			continue
		}
		c.put(string(key), cloneBytes(value), now, expires)
	}
	if len(rest) != 0 {
		return 0, ErrCorruptLog
	}
	return seq, nil
}

func (c *Cache) replay() error {
	path := filepath.Join(c.dir, aofName)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		rec, err := readRecord(f)
		if err == io.EOF || errors.Is(err, errIncomplete) {
			return nil
		}
		if err != nil {
			return err
		}
		if rec.seq <= c.seq {
			continue
		}
		c.apply(rec)
		c.seq = rec.seq
	}
}

func (c *Cache) apply(rec record) {
	now := c.now()
	switch rec.op {
	case opSet:
		if !rec.expires.IsZero() && !now.Before(rec.expires) {
			if e, ok := c.items[rec.key]; ok {
				c.dropExpired(e)
			}
			return
		}
		c.put(rec.key, cloneBytes(rec.value), now, rec.expires)
	case opDelete:
		if e, ok := c.items[rec.key]; ok {
			c.remove(e)
		}
	case opClear:
		c.items = make(map[string]*entry)
		c.head = nil
		c.tail = nil
		c.bytes = 0
	default:
		// Unknown operations are rejected by readRecord.
	}
}

func (c *Cache) rotateLogLocked() error {
	path := filepath.Join(c.dir, aofName)
	if c.aof != nil {
		if err := c.aof.Sync(); err != nil {
			return err
		}
		if err := c.aof.Close(); err != nil {
			return err
		}
		c.aof = nil
	}
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		_ = c.reopenLogLocked()
		return err
	}
	return c.reopenLogLocked()
}

func (c *Cache) reopenLogLocked() error {
	f, err := os.OpenFile(filepath.Join(c.dir, aofName), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	c.aof = f
	return nil
}

func (c *Cache) appendRecord(op byte, key string, value []byte, expires time.Time) error {
	if c.aof == nil {
		return nil
	}
	if op == opSet && len(value) > maxPersistValue {
		return ErrValueUnloggable
	}
	c.seq++
	rec := encodeRecord(c.seq, op, key, value, expires)
	if _, err := c.aof.Write(rec); err != nil {
		return err
	}
	if c.syncWrites {
		return c.aof.Sync()
	}
	return nil
}

type record struct {
	seq     uint64
	op      byte
	key     string
	value   []byte
	expires time.Time
}

func encodeRecord(seq uint64, op byte, key string, value []byte, expires time.Time) []byte {
	payload := make([]byte, 0, 32+len(key)+len(value))
	var seqBuf [8]byte
	binary.BigEndian.PutUint64(seqBuf[:], seq)
	payload = append(payload, seqBuf[:]...)
	payload = append(payload, op)
	switch op {
	case opSet:
		payload = append(payload, counted(key)...)
		payload = append(payload, countedBytes(value)...)
		payload = append(payload, encodeExpires(expires)...)
	case opDelete:
		payload = append(payload, counted(key)...)
	}
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(payload))
	return append(payload, crc[:]...)
}

func readRecord(r io.Reader) (record, error) {
	var seqBuf [8]byte
	_, err := io.ReadFull(r, seqBuf[:])
	if err == io.EOF {
		return record{}, io.EOF
	}
	if err == io.ErrUnexpectedEOF {
		return record{}, errIncomplete
	}
	if err != nil {
		return record{}, err
	}
	payload := append([]byte{}, seqBuf[:]...)
	var op [1]byte
	if err := readExact(r, op[:], &payload); err != nil {
		return record{}, err
	}
	rec := record{seq: binary.BigEndian.Uint64(seqBuf[:]), op: op[0]}
	switch op[0] {
	case opSet:
		key, err := readCounted(r, &payload, MaxKeyBytes)
		if err != nil {
			return record{}, err
		}
		value, err := readCounted(r, &payload, maxPersistValue)
		if err != nil {
			return record{}, err
		}
		expires, err := readExpires(r, &payload)
		if err != nil {
			return record{}, err
		}
		rec.key = string(key)
		rec.value = value
		rec.expires = expires
	case opDelete:
		key, err := readCounted(r, &payload, MaxKeyBytes)
		if err != nil {
			return record{}, err
		}
		rec.key = string(key)
	case opClear:
	default:
		return record{}, ErrCorruptLog
	}
	var crc [4]byte
	if _, err := io.ReadFull(r, crc[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return record{}, errIncomplete
		}
		return record{}, err
	}
	if binary.BigEndian.Uint32(crc[:]) != crc32.ChecksumIEEE(payload) {
		return record{}, ErrCorruptLog
	}
	return rec, nil
}

func readExact(r io.Reader, buf []byte, payload *[]byte) error {
	_, err := io.ReadFull(r, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return errIncomplete
	}
	if err != nil {
		return err
	}
	*payload = append(*payload, buf...)
	return nil
}

func readCounted(r io.Reader, payload *[]byte, limit int) ([]byte, error) {
	var nbuf [4]byte
	if err := readExact(r, nbuf[:], payload); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(nbuf[:])
	if int(n) > limit {
		return nil, ErrCorruptLog
	}
	buf := make([]byte, n)
	if n == 0 {
		return buf, nil
	}
	if err := readExact(r, buf, payload); err != nil {
		return nil, err
	}
	return buf, nil
}

func readExpires(r io.Reader, payload *[]byte) (time.Time, error) {
	var flag [1]byte
	if err := readExact(r, flag[:], payload); err != nil {
		return time.Time{}, err
	}
	if flag[0] == 0 {
		return time.Time{}, nil
	}
	if flag[0] != 1 {
		return time.Time{}, ErrCorruptLog
	}
	var buf [8]byte
	if err := readExact(r, buf[:], payload); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, int64(binary.BigEndian.Uint64(buf[:]))), nil
}

func counted(s string) []byte {
	return countedBytes([]byte(s))
}

func countedBytes(b []byte) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(b)))
	out := make([]byte, 0, 4+len(b))
	out = append(out, n[:]...)
	return append(out, b...)
}

func encodeExpires(t time.Time) []byte {
	if t.IsZero() {
		return []byte{0}
	}
	out := make([]byte, 9)
	out[0] = 1
	binary.BigEndian.PutUint64(out[1:], uint64(t.UnixNano()))
	return out
}

func takeCounted(in []byte, limit int) ([]byte, []byte, error) {
	if len(in) < 4 {
		return nil, nil, ErrCorruptLog
	}
	n := binary.BigEndian.Uint32(in[:4])
	if int(n) > limit || len(in) < 4+int(n) {
		return nil, nil, ErrCorruptLog
	}
	return in[4 : 4+n], in[4+n:], nil
}

func takeExpires(in []byte) (time.Time, []byte, error) {
	if len(in) < 1 {
		return time.Time{}, nil, ErrCorruptLog
	}
	if in[0] == 0 {
		return time.Time{}, in[1:], nil
	}
	if in[0] != 1 || len(in) < 9 {
		return time.Time{}, nil, ErrCorruptLog
	}
	n := int64(binary.BigEndian.Uint64(in[1:9]))
	return time.Unix(0, n), in[9:], nil
}
