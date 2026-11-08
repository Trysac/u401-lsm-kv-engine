package lsmkvengine

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"math"
	"sync"
	"sync/atomic"
)

// SSTable format constants:
// Footer is 48 bytes:
// [8 bytes index_offset] [8 bytes index_len]
// [8 bytes meta_offset]  [8 bytes meta_len] (Bloom filter)
// [8 bytes entries_count][4 bytes crc32c] [4 bytes magic: 0x4C534D31]
const (
	sstableMagic     uint32 = 0x4C534D31
	footerSize       int    = 48
	defaultBlockSize int    = 4096
)

// SSTableWriter writes entries sequentially into an immutable SSTable file.
type SSTableWriter struct {
	w          io.Writer
	offset     int64
	keys       [][]byte
	currBlock  bytes.Buffer
	indexBlock bytes.Buffer
	firstKey   []byte
	lastKey    []byte
	entries    int64
	blockSize  int
	bitsPerKey int
}

// NewSSTableWriter initializes an SSTableWriter.
func NewSSTableWriter(w io.Writer, bitsPerKey int) *SSTableWriter {
	if bitsPerKey <= 0 {
		bitsPerKey = 10
	}
	return &SSTableWriter{
		w:          w,
		blockSize:  defaultBlockSize,
		bitsPerKey: bitsPerKey,
	}
}

// Add writes a key/value/deleted entry to the SSTable. Entries must be added in ascending order.
func (sw *SSTableWriter) Add(key, val []byte, deleted bool) error {
	kLen := len(key)
	if kLen < 0 || kLen > math.MaxUint16 || kLen > MaxKeyBytes {
		return ErrKeyTooLarge
	}
	vLen := len(val)
	if vLen < 0 || uint64(vLen) > math.MaxUint32 || vLen > MaxValueBytes {
		return ErrValTooLarge
	}
	if len(sw.firstKey) == 0 {
		sw.firstKey = append([]byte(nil), key...)
	}
	sw.lastKey = append([]byte(nil), key...)
	sw.keys = append(sw.keys, append([]byte(nil), key...))
	if sw.entries < 0 {
		return newLSMError("corrupt_sstable", "invalid entries count", nil)
	}
	sw.entries++

	// Entry encoding:
	// [2 bytes key_len] [4 bytes val_len] [1 byte deleted] [key] [val]
	rec := make([]byte, 2+4+1+kLen+vLen)
	binary.LittleEndian.PutUint16(rec[0:2], uint16(kLen&0xffff))
	binary.LittleEndian.PutUint32(rec[2:6], uint32(vLen&0xffffffff))
	if deleted {
		rec[6] = 1
	} else {
		rec[6] = 0
	}
	copy(rec[7:7+len(key)], key)
	copy(rec[7+len(key):], val)

	sw.currBlock.Write(rec)
	if sw.currBlock.Len() >= sw.blockSize {
		if err := sw.flushDataBlock(); err != nil {
			return err
		}
	}
	return nil
}

func (sw *SSTableWriter) flushDataBlock() error {
	if sw.currBlock.Len() == 0 {
		return nil
	}
	blockBytes := sw.currBlock.Bytes()
	blockOffset := sw.offset
	blockLen := int64(len(blockBytes))

	// Calculate checksum
	crc := crc32.Checksum(blockBytes, crc32Table)
	crcBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(crcBuf, crc)

	if _, err := sw.w.Write(blockBytes); err != nil {
		return err
	}
	if _, err := sw.w.Write(crcBuf); err != nil {
		return err
	}
	sw.offset += blockLen + 4

	// Record index entry: [2 bytes key_len] [key] [8 bytes offset] [8 bytes len]
	lkLen := len(sw.lastKey)
	if lkLen < 0 || lkLen > math.MaxUint16 || lkLen > MaxKeyBytes || blockOffset < 0 || blockLen < 0 {
		return newLSMError("corrupt_sstable", "invalid block offset or length", nil)
	}
	idxRec := make([]byte, 2+lkLen+8+8)
	binary.LittleEndian.PutUint16(idxRec[0:2], uint16(lkLen&0xffff))
	copy(idxRec[2:2+len(sw.lastKey)], sw.lastKey)
	binary.LittleEndian.PutUint64(idxRec[2+len(sw.lastKey):2+len(sw.lastKey)+8], uint64(blockOffset))
	binary.LittleEndian.PutUint64(idxRec[2+len(sw.lastKey)+8:], uint64(blockLen))
	sw.indexBlock.Write(idxRec)

	sw.currBlock.Reset()
	return nil
}

// Finish finalizes the SSTable and writes index, meta (Bloom filter), and footer.
func (sw *SSTableWriter) Finish() error {
	if err := sw.flushDataBlock(); err != nil {
		return err
	}

	// Write index block
	indexOffset := sw.offset
	indexBytes := sw.indexBlock.Bytes()
	indexLen := int64(len(indexBytes))
	if _, err := sw.w.Write(indexBytes); err != nil {
		return err
	}
	sw.offset += indexLen

	// Write meta block (Bloom filter)
	metaOffset := sw.offset
	bf := NewBloomFilter(sw.keys, sw.bitsPerKey)
	metaBytes := bf.Encode()
	metaLen := int64(len(metaBytes))
	if _, err := sw.w.Write(metaBytes); err != nil {
		return err
	}
	sw.offset += metaLen

	if indexOffset < 0 || indexLen < 0 || metaOffset < 0 || metaLen < 0 || sw.entries < 0 {
		return newLSMError("corrupt_sstable", "invalid negative footer values", nil)
	}

	// Build footer
	footer := make([]byte, footerSize)
	binary.LittleEndian.PutUint64(footer[0:8], uint64(indexOffset))
	binary.LittleEndian.PutUint64(footer[8:16], uint64(indexLen))
	binary.LittleEndian.PutUint64(footer[16:24], uint64(metaOffset))
	binary.LittleEndian.PutUint64(footer[24:32], uint64(metaLen))
	binary.LittleEndian.PutUint64(footer[32:40], uint64(sw.entries))
	binary.LittleEndian.PutUint32(footer[44:48], sstableMagic)

	crc := crc32.Checksum(footer[0:40], crc32Table)
	binary.LittleEndian.PutUint32(footer[40:44], crc)

	if _, err := sw.w.Write(footer); err != nil {
		return err
	}
	sw.offset += int64(footerSize)
	return nil
}

// IndexEntry points to a data block in an SSTable.
type IndexEntry struct {
	Key    []byte
	Offset int64
	Length int64
}

// SSTableReader provides concurrent reads over an immutable SSTable file.
type SSTableReader struct {
	file     File
	fileSize int64
	index    []IndexEntry
	bloom    *BloomFilter
	entries  int64
	refCount atomic.Int32
	mu       sync.RWMutex
	closed   bool
}

// OpenSSTableReader opens and verifies an SSTable file.
func OpenSSTableReader(f File) (*SSTableReader, error) {
	stat, err := f.Stat()
	if err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to stat sstable", err)
	}
	sz := stat.Size()
	if sz < int64(footerSize) {
		return nil, newLSMError("corrupt_sstable", "sstable file smaller than footer", nil)
	}

	footer := make([]byte, footerSize)
	if _, err := f.ReadAt(footer, sz-int64(footerSize)); err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to read sstable footer", err)
	}

	magic := binary.LittleEndian.Uint32(footer[44:48])
	if magic != sstableMagic {
		return nil, newLSMError("corrupt_sstable", "invalid sstable magic", nil)
	}

	crcExpected := binary.LittleEndian.Uint32(footer[40:44])
	if crc32.Checksum(footer[0:40], crc32Table) != crcExpected {
		return nil, newLSMError("corrupt_sstable", "footer crc checksum mismatch", nil)
	}

	uIdxOffset := binary.LittleEndian.Uint64(footer[0:8])
	uIdxLen := binary.LittleEndian.Uint64(footer[8:16])
	uMetaOffset := binary.LittleEndian.Uint64(footer[16:24])
	uMetaLen := binary.LittleEndian.Uint64(footer[24:32])
	uEntries := binary.LittleEndian.Uint64(footer[32:40])
	const maxInt64 = ^uint64(0) >> 1
	if uIdxOffset > maxInt64 || uIdxLen > maxInt64 || uMetaOffset > maxInt64 || uMetaLen > maxInt64 || uEntries > maxInt64 {
		return nil, newLSMError("corrupt_sstable", "integer overflow in footer dimensions", nil)
	}

	idxOffset := int64(uIdxOffset)
	idxLen := int64(uIdxLen)
	metaOffset := int64(uMetaOffset)
	metaLen := int64(uMetaLen)
	entries := int64(uEntries)

	if idxOffset < 0 || idxLen < 0 || idxOffset+idxLen > sz-int64(footerSize) {
		return nil, newLSMError("corrupt_sstable", "invalid index block bounds", nil)
	}
	if metaOffset < 0 || metaLen < 0 || metaOffset+metaLen > sz-int64(footerSize) {
		return nil, newLSMError("corrupt_sstable", "invalid meta block bounds", nil)
	}

	// Read Index
	idxBytes := make([]byte, idxLen)
	if _, err := f.ReadAt(idxBytes, idxOffset); err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to read index block", err)
	}
	index, err := parseIndex(idxBytes)
	if err != nil {
		return nil, err
	}

	// Read Meta (Bloom filter)
	metaBytes := make([]byte, metaLen)
	if _, err := f.ReadAt(metaBytes, metaOffset); err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to read bloom filter", err)
	}
	bf, err := DecodeBloomFilter(metaBytes)
	if err != nil {
		return nil, err
	}

	r := &SSTableReader{
		file:     f,
		fileSize: sz,
		index:    index,
		bloom:    bf,
		entries:  entries,
	}
	r.refCount.Store(1)
	return r, nil
}

func parseIndex(data []byte) ([]IndexEntry, error) {
	var res []IndexEntry
	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			return nil, newLSMError("corrupt_sstable", "truncated index key length", nil)
		}
		kLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+kLen+16 > len(data) {
			return nil, newLSMError("corrupt_sstable", "truncated index record", nil)
		}
		k := make([]byte, kLen)
		copy(k, data[offset:offset+kLen])
		offset += kLen
		uBlkOff := binary.LittleEndian.Uint64(data[offset : offset+8])
		offset += 8
		uBlkLen := binary.LittleEndian.Uint64(data[offset : offset+8])
		offset += 8
		const maxInt64 = ^uint64(0) >> 1
		if uBlkOff > maxInt64 || uBlkLen > maxInt64 {
			return nil, newLSMError("corrupt_sstable", "integer overflow in index block offsets", nil)
		}
		blkOff := int64(uBlkOff)
		blkLen := int64(uBlkLen)
		res = append(res, IndexEntry{Key: k, Offset: blkOff, Length: blkLen})
	}
	return res, nil
}

// IncRef increments ref count for active iterators.
func (r *SSTableReader) IncRef() {
	r.refCount.Add(1)
}

// DecRef decrements ref count and closes file when zero.
func (r *SSTableReader) DecRef() error {
	if r.refCount.Add(-1) == 0 {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !r.closed {
			r.closed = true
			return r.file.Close()
		}
	}
	return nil
}

// Close closes reader.
func (r *SSTableReader) Close() error {
	return r.DecRef()
}

func (r *SSTableReader) readDataBlock(ie IndexEntry) ([]byte, error) {
	buf := make([]byte, ie.Length+4)
	if _, err := r.file.ReadAt(buf, ie.Offset); err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to read data block", err)
	}
	blockBytes := buf[:ie.Length]
	crcRead := binary.LittleEndian.Uint32(buf[ie.Length:])
	if crc32.Checksum(blockBytes, crc32Table) != crcRead {
		return nil, newLSMError("corrupt_sstable", "block checksum mismatch", nil)
	}
	return blockBytes, nil
}

// Get looks up a key in the SSTable.
// Returns (value, found, isDeleted, error).
func (r *SSTableReader) Get(key []byte) ([]byte, bool, bool, error) {
	if r.bloom != nil && !r.bloom.MayContain(key) {
		return nil, false, false, nil
	}

	for _, ie := range r.index {
		if bytes.Compare(key, ie.Key) <= 0 {
			block, err := r.readDataBlock(ie)
			if err != nil {
				return nil, false, false, err
			}
			off := 0
			for off < len(block) {
				if off+7 > len(block) {
					return nil, false, false, newLSMError("corrupt_sstable", "malformed entry header in data block", nil)
				}
				kLen := int(binary.LittleEndian.Uint16(block[off : off+2]))
				vLen := int(binary.LittleEndian.Uint32(block[off+2 : off+6]))
				deleted := block[off+6] != 0
				off += 7
				if off+kLen > len(block) || off+kLen+vLen > len(block) {
					return nil, false, false, newLSMError("corrupt_sstable", "malformed entry body in data block", nil)
				}
				k := block[off : off+kLen]
				v := block[off+kLen : off+kLen+vLen]
				off += kLen + vLen

				if bytes.Equal(k, key) {
					res := make([]byte, len(v))
					copy(res, v)
					return res, true, deleted, nil
				}
			}
		}
	}
	return nil, false, false, nil
}

// ReadAllEntries reads all entries from the SSTable.
func (r *SSTableReader) ReadAllEntries() ([]MemEntry, error) {
	var res []MemEntry
	for _, ie := range r.index {
		block, err := r.readDataBlock(ie)
		if err != nil {
			return nil, err
		}
		off := 0
		for off < len(block) {
			if off+7 > len(block) {
				return nil, newLSMError("corrupt_sstable", "malformed entry header in data block", nil)
			}
			kLen := int(binary.LittleEndian.Uint16(block[off : off+2]))
			vLen := int(binary.LittleEndian.Uint32(block[off+2 : off+6]))
			deleted := block[off+6] != 0
			off += 7
			if off+kLen > len(block) || off+kLen+vLen > len(block) {
				return nil, newLSMError("corrupt_sstable", "malformed entry body in data block", nil)
			}
			k := append([]byte(nil), block[off:off+kLen]...)
			v := append([]byte(nil), block[off+kLen:off+kLen+vLen]...)
			off += kLen + vLen
			res = append(res, MemEntry{Key: k, Val: v, Deleted: deleted})
		}
	}
	return res, nil
}
