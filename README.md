# personalkafka

A Kafka-style message log written from scratch in Go: a durable, append-only storage engine with a TCP message broker on top.

Producers send records over a custom binary protocol; the broker appends them to partitioned on-disk logs and serves them back to consumers by offset.

## Features

- **Storage engine:** segmented append-only log with CRC-32C checksummed records and a sparse offset index for fast lookups
- **Crash recovery:** detects and truncates torn writes on startup, verified by a kill -9 crash-test harness
- **Retention:** deletes old segments by total size or age
- **Broker:** concurrent TCP server with length-prefixed framing, produce and fetch requests, and graceful shutdown
- **Topics and partitions:** key-hash partitioning in the client library
- **Consumer offsets:** committed offsets persisted in an internal topic and replayed on startup
- **Configurable durability:** per-write or interval fsync, with throughput and p50/p99 latency benchmarks for each

## Status

In progress. The record format (encoding, decoding, corruption detection) is complete; the storage engine is under construction.
