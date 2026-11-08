# Testing

## Test Suite
- **Unit Tests**: Verify individual components (MemTable, WAL, SSTable).
- **Property Tests**: `testing/quick` with fixed seeds validates engine logic against an in-memory sorted map model.
- **Fuzz Tests**: `go test -fuzz` targets run against reader/writer interfaces to ensure no panics on corrupt input (fuzzing `SSTableReader` and `WALReader`).
- **Conformance**: Runs on local gates and CI. Validates `Put`/`Delete`/`Get`/`Scan`/`Batch` sequences and crash recovery simulations (dropping unsynced bytes).

## Conformance exclusions
None.