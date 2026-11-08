package lsmkvengine

// BatchOperation represents a single write action inside a Batch.
type BatchOperation struct {
	Type  byte
	Key   []byte
	Value []byte
}

// Batch allows atomic application of multiple mutations.
type Batch struct {
	ops []BatchOperation
}

// NewBatch creates an empty batch.
func NewBatch() *Batch {
	return &Batch{}
}

// Put appends a put operation.
func (b *Batch) Put(key, value []byte) {
	b.ops = append(b.ops, BatchOperation{
		Type:  recordPut,
		Key:   append([]byte(nil), key...),
		Value: append([]byte(nil), value...),
	})
}

// Delete appends a delete operation.
func (b *Batch) Delete(key []byte) {
	b.ops = append(b.ops, BatchOperation{
		Type:  recordDelete,
		Key:   append([]byte(nil), key...),
		Value: nil,
	})
}

// Clear resets all operations in the batch.
func (b *Batch) Clear() {
	b.ops = b.ops[:0]
}

// Len returns the count of operations.
func (b *Batch) Len() int {
	return len(b.ops)
}
