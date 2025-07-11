package clickhouse_store

var createLogsTableTemplate = `
CREATE TABLE IF NOT EXISTS %s (
    id UUID CODEC(ZSTD),
    store_id UInt32 CODEC(ZSTD),
    source_id UInt32 CODEC(ZSTD),
    minted_at DateTime64(3, 'UTC') CODEC(ZSTD),
    block_hash String CODEC(ZSTD),
    block_number UInt64 CODEC(ZSTD),
    transaction_from String CODEC(ZSTD),
    transaction_hash String CODEC(ZSTD),
    transaction_index UInt32 CODEC(ZSTD),
    removed Bool CODEC(ZSTD),
    log_index UInt32 CODEC(ZSTD),
    address String CODEC(ZSTD),
    data String CODEC(ZSTD),
    topics Array(String) CODEC(ZSTD),
    metadata JSON CODEC(ZSTD),

    index idx_id id type bloom_filter granularity 1,
    index idx_store_id store_id type bloom_filter granularity 1,
    index idx_source_id source_id type bloom_filter granularity 1,
    index idx_minted_at minted_at type minmax granularity 1,
    index idx_block_hash block_hash type bloom_filter granularity 4,
    index idx_transaction_from transaction_from type bloom_filter granularity 4,
    index idx_transaction_hash transaction_hash type bloom_filter granularity 4,
    index idx_address address type bloom_filter granularity 1,
    index idx_topics_1 topics[1] type bloom_filter granularity 4
)
engine = ReplacingMergeTree
partition by toYYYYMM(minted_at)
order by (block_number, log_index)
`

var createTransactionsTableTemplate = `
CREATE TABLE IF NOT EXISTS %s (
    id UUID CODEC(ZSTD),
    store_id UInt32 CODEC(ZSTD),
    source_id UInt32 CODEC(ZSTD),
    minted_at DateTime64(3, 'UTC') CODEC(ZSTD),
    
    block_number UInt64 CODEC(ZSTD),
    transaction_index UInt64 CODEC(ZSTD),
    chain_id UInt32 CODEC(ZSTD),
    from String CODEC(ZSTD),
    hash String CODEC(ZSTD),
    data String CODEC(ZSTD),
    nonce UInt64 CODEC(ZSTD),
    to String CODEC(ZSTD),
    value UInt256 CODEC(ZSTD),

    metadata JSON CODEC(ZSTD),

    index idx_minted_at minted_at type minmax granularity 1,
    index idx_block_number block_number type minmax granularity 4,
    index idx_transaction_index transaction_index type minmax granularity 4,
    index idx_from from type bloom_filter granularity 4,
    index idx_hash hash type bloom_filter granularity 4,
)
engine = ReplacingMergeTree
partition by toYYYYMM(minted_at)
order by (block_number, transaction_index)
`
