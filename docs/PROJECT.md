# Project Record

## Objective
Provide a zero-dependency, crash-resilient LSM-tree storage engine with deterministic snapshot isolation and verifiable durability.

## Scope
- Core: Skiplist memtable, WAL with CRC32C, immutable SSTable reader/writer with block restarts & Bloom filters, level compaction (L0 to L1), manifest version tracking, crash recovery with truncated tail tolerance, snapshot iterators.
- CLI: `lsmctl` utility supporting `get`, `put`, `scan`, `stats`, and `check`.
- Metrics: Prometheus exports via `internal/metrics` (using `u135-exporter-kit`).

## Out of Scope
- Multi-key distributed transactions.
- Cgo-based compression (LZ4/Snappy).
- Network server protocols.
- Column families.
- Leveled compaction beyond L1.

## Line comparison metrics

| Model | Line | Fix rounds | Input tokens | Output tokens | Cost | Time | Failing checks | Conformance % | Integrations |
|---|---|---|---|---|---|---|---|---|---|

## Factory run u401 (measured from API usage)

| Phase | Model | Batch | Input | Output | Cache write | Cache read | Cost (USD) |
|---|---|---|---|---|---|---|---|
| analyst | gemini-3.8-flash | no | 3735 | 1238 | 0 | 0 | 0.0074 |
| generation | gemini-3.8-flash | yes | 7166 | 31913 | 0 | 0 | 0.0625 |
| fix-1 | gemini-3.8-flash | no | 148444 | 1943 | 0 | 1891637 | 0.2605 |
| fix-2 | gemini-3.8-flash | no | 143107 | 2184 | 0 | 1860398 | 0.2551 |
| fix-3 | gemini-3.8-flash | no | 155109 | 6137 | 0 | 2340883 | 0.3149 |
| fix-4 | gemini-3.8-flash | no | 72882 | 1075 | 0 | 556985 | 0.1005 |
| fix-5 | gemini-3.8-flash | no | 65902 | 1665 | 0 | 478179 | 0.0915 |
| fix-6 | gemini-3.8-flash | no | 146939 | 1658 | 0 | 2159465 | 0.2784 |
| fix-1 | gemini-3.8-flash | no | 154048 | 4105 | 0 | 2160601 | 0.2930 |
| fix-2 | gemini-3.8-flash | no | 125325 | 7507 | 0 | 1586978 | 0.2412 |
| fix-3 | gemini-3.8-flash | no | 111008 | 6106 | 0 | 1406641 | 0.2117 |
| fix-4 | gemini-3.8-flash | no | 62868 | 457 | 0 | 395995 | 0.0786 |
| audit | gemini-3.8-flash | no | 128538 | 1462 | 0 | 1108763 | 0.1850 |
| docs | gemini-3.1-flash-lite | no | 3832 | 1290 | 0 | 0 | 0.0029 |

Total: $2.3831. Prices: Anthropic list prices.
