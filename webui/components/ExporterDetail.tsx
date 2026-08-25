"use client";

import { useEffect, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { client } from "@/lib/client";
import type { EvmiExporter, EvmiExporterSourceCursor } from "@/gen/evm_indexer/v1/evm_indexer_pb";

// The tsconfig target predates BigInt literals (0n), so compare against this.
const ZERO = BigInt(0);

// ExporterDetail shows how far an exporter has exported *each* source of its
// pipeline. That is the real progress picture: an exporter keeps one cursor per
// source (they advance independently, and a source attached at runtime by a
// factory rule or a plugin starts from its own first block), so the exporter's own
// sync block is only the minimum across the rows below.
//
// It loads the current cursors once, then follows the server stream, which emits
// one event per source per exported batch.
export default function ExporterDetail({ item }: { item: EvmiExporter }) {
  const exporterId = item.id ?? 0;
  const [cursors, setCursors] = useState<EvmiExporterSourceCursor[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [live, setLive] = useState(false);

  useEffect(() => {
    if (!exporterId) return;
    let cancelled = false;
    client
      .listEvmiExporterSourceCursors({ exporterId })
      .then((r) => {
        if (!cancelled) setCursors(r.cursors ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof ConnectError ? e.message : "failed to load cursors");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [exporterId]);

  // Live updates, merged by source id, with auto-reconnect — the same shape the
  // resource list streams use.
  useEffect(() => {
    if (!exporterId) return;
    const controller = new AbortController();
    const signal = controller.signal;

    void (async () => {
      while (!signal.aborted) {
        try {
          for await (const cursor of client.streamEvmiExporterSourceCursors({ exporterId }, { signal })) {
            if (signal.aborted) return;
            setLive(true);
            setCursors((prev) => {
              const next = prev.filter((c) => c.sourceId !== cursor.sourceId);
              next.push(cursor);
              next.sort((a, b) => a.sourceId - b.sourceId);
              return next;
            });
          }
        } catch {
          // disconnected; retry below unless aborted.
        }
        if (signal.aborted) return;
        setLive(false);
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    })();

    return () => controller.abort();
  }, [exporterId]);

  const totals = useMemo(() => {
    if (cursors.length === 0) return null;
    const lag = cursors.reduce((max, c) => (c.lagBlocks > max ? c.lagBlocks : max), ZERO);
    const behind = cursors.filter((c) => c.lagBlocks > ZERO).length;
    return { lag, behind };
  }, [cursors]);

  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between", marginBottom: 4 }}>
        <div>
          <strong>{item.name}</strong>{" "}
          <span className="muted">
            · pipeline {item.evmLogPipelineId} · aggregate sync block {String(item.syncBlock)}
          </span>
        </div>
        {live && <span className="live">● live</span>}
      </div>

      <p className="muted" style={{ marginTop: 0, fontSize: 13 }}>
        One cursor per source. The exporter&apos;s sync block above is the minimum of the{" "}
        <em>exported</em> column.
      </p>

      {error && <div className="error banner">{error}</div>}

      {loading ? (
        <div className="empty muted">Loading…</div>
      ) : cursors.length === 0 ? (
        <div className="empty">
          <p className="muted">This exporter&apos;s pipeline has no sources.</p>
        </div>
      ) : (
        <>
          {totals && (
            <p className="muted" style={{ fontSize: 13 }}>
              {cursors.length} source{cursors.length === 1 ? "" : "s"} ·{" "}
              {totals.behind === 0 ? "all caught up" : `${totals.behind} behind · max lag ${String(totals.lag)} blocks`}
            </p>
          )}
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Source</th>
                  <th>Type</th>
                  <th>Address / topic</th>
                  <th>Exported</th>
                  <th>Indexed</th>
                  <th>Lag</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {cursors.map((c) => (
                  <tr key={c.sourceId}>
                    <td>
                      #{c.sourceId}
                      {c.parentSourceId !== 0 && (
                        <span className="muted" title={`created by source #${c.parentSourceId}`}>
                          {" "}
                          ↳ {c.parentSourceId}
                        </span>
                      )}
                    </td>
                    <td>{c.sourceType}</td>
                    <td className="mono" title={c.sourceAddress || c.sourceTopic0 || ""}>
                      {shorten(c.sourceAddress || c.sourceTopic0)}
                    </td>
                    <td className="mono">{formatCursor(c)}</td>
                    <td className="mono">{String(c.sourceSyncBlock)}</td>
                    <td>
                      <span className={`badge ${lagTone(c.lagBlocks)}`}>{String(c.lagBlocks)}</span>
                    </td>
                    <td>
                      {!c.sourceEnabled ? (
                        <span className="badge badge-muted">disabled</span>
                      ) : (
                        <span className={`badge ${statusTone(c.sourceStatus)}`}>{c.sourceStatus || "—"}</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

// formatCursor renders the (block, log index) pair, showing the in-progress block
// only when the exporter actually stopped inside one (log index >= 0).
function formatCursor(c: EvmiExporterSourceCursor): string {
  if (c.syncLogIndex < ZERO) return String(c.syncBlock);
  // Mid-block: block N is done and N+1 is partially delivered, up to this index.
  return `${String(c.syncBlock)} (+${String(c.syncBlock + BigInt(1))}:${String(c.syncLogIndex)})`;
}

function lagTone(lag: bigint): string {
  if (lag === ZERO) return "badge-ok";
  if (lag < BigInt(1000)) return "badge-neutral";
  return "badge-warn";
}

function statusTone(status: string): string {
  if (status === "RUNNING") return "badge-ok";
  if (status === "FAILED") return "badge-danger";
  return "badge-neutral";
}

function shorten(hex: string): string {
  if (!hex) return "—";
  if (hex.length <= 14) return hex;
  return `${hex.slice(0, 8)}…${hex.slice(-6)}`;
}
