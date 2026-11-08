# Security and Threat Model

## Defended Threats
- **Sudden power cut / crash mid-write**: WAL uses CRC32C verification; truncated final records at EOF are tolerated on replay.
- **Corrupted disk blocks**: SSTables use block-level checksum validation.
- **Concurrent directory access**: Single-process lock via `LOCK` file prevents multi-process corruption.
- **Stale reads**: Iterators use ref-counted SSTables to prevent unlinking active file handles.

## Non-defended Threats
- **Byzantine corruption**: Hardware bitflips occurring in RAM are not detected.
- **Unsafe concurrent access**: Multi-process writes to the same directory without proper external synchronization are not guarded beyond the lockfile.