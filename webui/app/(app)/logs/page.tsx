"use client";

import { useCallback, useEffect, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { client } from "@/lib/client";
import type { EvmLog, EvmLogSource } from "@/gen/evm_indexer/v1/evm_indexer_pb";

// Read-only viewer for stored logs: pick a source and see its most recent decoded
// logs, including the block timestamp indexed from the block header.
export default function LogsPage() {
  const [sources, setSources] = useState<EvmLogSource[]>([]);
  // ABI contract name by id, so the source picker can name what a source decodes
  // instead of showing only its address.
  const [abiNames, setAbiNames] = useState<Record<number, string>>({});
  const [sourceId, setSourceId] = useState<number>(0);
  const [limit, setLimit] = useState<number>(100);
  const [logs, setLogs] = useState<EvmLog[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    client
      .listEvmLogSources({ pagination: { offset: 0, limit: 200 } })
      .then((r) => {
        const list = r.sources ?? [];
        setSources(list);
        if (list.length && !sourceId) setSourceId(list[0].id ?? 0);
      })
      .catch((e) => setError(e instanceof ConnectError ? e.message : "failed to load sources"));

    // Best-effort: without the names the picker still works, just less readable.
    client
      .listEvmJsonAbis({ pagination: { offset: 0, limit: 200 } })
      .then((r) => {
        const names: Record<number, string> = {};
        for (const abi of r.abis ?? []) {
          if (abi.id) names[abi.id] = abi.contractName;
        }
        setAbiNames(names);
      })
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // "#3 · CONTRACT · UniswapV2Pair · 0x1234…abcdef" — the ABI name is dropped when
  // the source has none (a FULL source, or one indexing raw logs).
  const sourceLabel = useCallback(
    (s: EvmLogSource) => {
      const abi = abiNames[s.evmJsonAbiId];
      return [`#${s.id}`, s.type, abi, s.address || s.topic0 || "full"].filter(Boolean).join(" · ");
    },
    [abiNames],
  );

  const load = useCallback(async () => {
    if (!sourceId) return;
    setLoading(true);
    setError(null);
    try {
      const r = await client.listLatestEvmLogs({ sourceId, limit: BigInt(limit) });
      setLogs(r.logs ?? []);
    } catch (e) {
      setError(e instanceof ConnectError ? e.message : "failed to load logs");
    } finally {
      setLoading(false);
    }
  }, [sourceId, limit]);

  useEffect(() => {
    if (sourceId) load();
  }, [sourceId, load]);

  return (
    <div>
      <div className="page-header">
        <h2>
          Logs
          {!loading && <span className="count">{logs.length}</span>}
        </h2>
        <div className="row" style={{ gap: 8 }}>
          <select value={sourceId} onChange={(e) => setSourceId(Number(e.target.value))}>
            {sources.length === 0 && <option value={0}>No sources</option>}
            {sources.map((s) => (
              <option key={s.id} value={s.id}>
                {sourceLabel(s)}
              </option>
            ))}
          </select>
          <select value={limit} onChange={(e) => setLimit(Number(e.target.value))}>
            {[50, 100, 250, 500].map((n) => (
              <option key={n} value={n}>
                last {n}
              </option>
            ))}
          </select>
          <button className="secondary" onClick={load} disabled={loading || !sourceId}>
            Refresh
          </button>
        </div>
      </div>

      {error && <div className="error banner">{error}</div>}

      {loading ? (
        <div className="empty muted">Loading…</div>
      ) : logs.length === 0 ? (
        <div className="empty">
          <p className="muted">No logs for this source yet.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Block</th>
                <th>Time (UTC)</th>
                <th>Event</th>
                <th>Address</th>
                <th>Tx hash</th>
                <th>Log #</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr key={l.id}>
                  <td>{String(l.blockNumber)}</td>
                  <td title={String(l.blockTimestamp)}>{formatTimestamp(l.blockTimestamp)}</td>
                  <td>{l.metadata?.eventName || <span className="muted">—</span>}</td>
                  <td className="mono" title={l.address}>
                    {shorten(l.address)}
                  </td>
                  <td className="mono" title={l.transactionHash}>
                    {shorten(l.transactionHash)}
                  </td>
                  <td>{String(l.logIndex)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// formatTimestamp renders a unix-seconds block timestamp as an ISO-ish UTC string.
function formatTimestamp(ts: bigint): string {
  if (!ts) return "—";
  const d = new Date(Number(ts) * 1000);
  return d.toISOString().replace("T", " ").replace(".000Z", " UTC");
}

function shorten(hex: string): string {
  if (!hex || hex.length <= 14) return hex || "—";
  return `${hex.slice(0, 8)}…${hex.slice(-6)}`;
}
