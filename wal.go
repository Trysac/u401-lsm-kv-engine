package lsmkvengine

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"math"
)

// WAL Record types
const (
	recordPut    byte = 1
	recordDelete byte = 2
)

// WAL header format:
// [4 bytes CRC32C] [2 bytes key_len] [4 bytes val_len] [1 byte record_type]
// Followed by: [key bytes] [value bytes]
const walHeaderLen = 4 + 2 + 4 + 1

var crc32Table = crc32.MakeTable(crc32.Castagnoli)

// WALWriter writes log records to an underlying file.
type WALWriter struct {
	w File
}

// NewWALWriter creates a WALWriter.
func NewWALWriter(w File) *WALWriter {
	return &WALWriter{w: w}
}

// Append writes a Put or Delete record.
func (w *WALWriter) Append(recType byte, key, val []byte) error {
	kLen := len(key)
	if kLen < 0 || kLen > math.MaxUint16 || kLen > MaxKeyBytes {
		return ErrKeyTooLarge
	}
	vLen := len(val)
	if vLen < 0 || uint64(vLen) > math.MaxUint32 || vLen > MaxValueBytes {
		return ErrValTooLarge
	}
	header := make([]byte, walHeaderLen)
	binary.LittleEndian.PutUint16(header[4:6], uint16(kLen&0xffff))
	binary.LittleEndian.PutUint32(header[6:10], uint32(vLen&0xffffffff))
	header[10] = recType

	h := crc32.New(crc32Table)
	_, _ = h.Write(header[4:])
	_, _ = h.Write(key)
	_, _ = h.Write(val)
	crc := h.Sum32()
	binary.LittleEndian.PutUint32(header[0:4], crc)

	if _, err := w.w.Write(header); err != nil {
		return err
	}
	if len(key) > 0 {
		if _, err := w.w.Write(key); err != nil {
			return err
		}
	}
	if len(val) > 0 {
		if _, err := w.w.Write(val); err != nil {
			return err
		}
	}
	return nil
}

// Sync flushes underlying writes to disk.
func (w *WALWriter) Sync() error {
	return w.w.Sync()
}

// Close closes the underlying file.
func (w *WALWriter) Close() error {
	return w.w.Close()
}

// WALEntry represents a recovered record from a WAL file.
type WALEntry struct {
	Type  byte
	Key   []byte
	Value []byte
}

// ReadWALEntries reads all valid WAL entries from r.
// If a truncated record is found at EOF, it is tolerated.
// If an invalid CRC or corrupt payload is encountered mid-file or before EOF,
// a corrupt_wal error is returned.
func ReadWALEntries(r io.Reader) ([]WALEntry, error) {
	var entries []WALEntry
	header := make([]byte, walHeaderLen)

	for {
		n, err := io.ReadFull(r, header)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			// Truncated trailing record at EOF: tolerated
			break
		}
		if err != nil {
			return nil, newLSMError("corrupt_wal", "error reading WAL header", err)
		}
		if n < walHeaderLen {
			break
		}

		crcExpected := binary.LittleEndian.Uint32(header[0:4])
		keyLen := binary.LittleEndian.Uint16(header[4:6])
		valLen := binary.LittleEndian.Uint32(header[6:10])
		recType := header[10]

		if recType != recordPut && recType != recordDelete {
			return nil, newLSMError("corrupt_wal", "invalid record type in WAL", nil)
		}

		if valLen > MaxValueBytes {
			return nil, newLSMError("corrupt_wal", "value length exceeds limit", nil)
		}

		bodyLen := int(keyLen) + int(valLen)
		body := make([]byte, bodyLen)
		n, err = io.ReadFull(r, body)
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			// Truncated tail record: tolerated
			break
		}
		if err != nil {
			return nil, newLSMError("corrupt_wal", "error reading WAL record body", err)
		}

		h := crc32.New(crc32Table)
		_, _ = h.Write(header[4:])
		_, _ = h.Write(body)
		if h.Sum32() != crcExpected {
			return nil, newLSMError("corrupt_wal", "wal crc32 checksum mismatch", nil)
		}

		key := make([]byte, keyLen)
		copy(key, body[:keyLen])
		val := make([]byte, valLen)
		copy(val, body[keyLen:])

		entries = append(entries, WALEntry{
			Type:  recType,
			Key:   key,
			Value: val,
		})
	}
	return entries, nil
}
