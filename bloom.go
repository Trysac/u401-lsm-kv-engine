package lsmkvengine

import (
	"hash/crc32"
)

// BloomFilter is a simple Bloom filter implementation for SSTables.
type BloomFilter struct {
	bits []byte
	k    uint8
}

// NewBloomFilter creates a bloom filter for keys with bitsPerKey ratio.
func NewBloomFilter(keys [][]byte, bitsPerKey int) *BloomFilter {
	if bitsPerKey < 1 {
		bitsPerKey = 10
	}
	n := len(keys)
	if n == 0 {
		return &BloomFilter{bits: make([]byte, 1), k: 1}
	}
	totalBits := n * bitsPerKey
	if totalBits < 64 {
		totalBits = 64
	}
	bytesCount := (totalBits + 7) / 8
	bits := make([]byte, bytesCount)

	k := uint8(float64(bitsPerKey) * 0.69)
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	for _, key := range keys {
		h1 := crc32.ChecksumIEEE(key)
		h2 := crc32.Checksum(key, crc32.MakeTable(crc32.Castagnoli))
		for i := uint8(0); i < k; i++ {
			h := uint64(h1) + uint64(i)*uint64(h2)
			bitPos := h % uint64(len(bits)*8)
			bits[bitPos/8] |= (1 << (bitPos % 8))
		}
	}
	return &BloomFilter{bits: bits, k: k}
}

// MayContain returns true if key might be in the set.
func (bf *BloomFilter) MayContain(key []byte) bool {
	if len(bf.bits) == 0 {
		return true
	}
	h1 := crc32.ChecksumIEEE(key)
	h2 := crc32.Checksum(key, crc32.MakeTable(crc32.Castagnoli))
	for i := uint8(0); i < bf.k; i++ {
		h := uint64(h1) + uint64(i)*uint64(h2)
		bitPos := h % uint64(len(bf.bits)*8)
		if (bf.bits[bitPos/8] & (1 << (bitPos % 8))) == 0 {
			return false
		}
	}
	return true
}

// Encode serializes the bloom filter.
func (bf *BloomFilter) Encode() []byte {
	res := make([]byte, 1+len(bf.bits))
	res[0] = bf.k
	copy(res[1:], bf.bits)
	return res
}

// DecodeBloomFilter deserializes a bloom filter from bytes.
func DecodeBloomFilter(data []byte) (*BloomFilter, error) {
	if len(data) < 1 {
		return nil, newLSMError("corrupt_sstable", "invalid bloom filter bytes", nil)
	}
	k := data[0]
	bits := make([]byte, len(data)-1)
	copy(bits, data[1:])
	return &BloomFilter{bits: bits, k: k}, nil
}
