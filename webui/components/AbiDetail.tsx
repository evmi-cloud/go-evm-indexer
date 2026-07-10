"use client";

import { useMemo } from "react";
import type { EvmJsonAbi } from "@/gen/evm_indexer/v1/evm_indexer_pb";

type AbiParam = { name?: string; type: string; indexed?: boolean; components?: AbiParam[] };
type AbiItem = {
  type: string;
  name?: string;
  inputs?: AbiParam[];
  outputs?: AbiParam[];
  stateMutability?: string;
  anonymous?: boolean;
};

function formatType(p: AbiParam): string {
  if (p.type.startsWith("tuple") && p.components) {
    const inner = p.components.map(formatType).join(", ");
    return `(${inner})${p.type.slice("tuple".length)}`;
  }
  return p.type;
}

function formatParam(p: AbiParam): string {
  const t = formatType(p);
  const idx = p.indexed ? " indexed" : "";
  return p.name ? `${t}${idx} ${p.name}` : `${t}${idx}`;
}

function mutabilityTone(m?: string): string {
  if (m === "view" || m === "pure") return "badge-neutral";
  if (m === "payable") return "badge-warn";
  return "badge-muted";
}

export default function AbiDetail({ item }: { item: EvmJsonAbi }) {
  const parsed = useMemo(() => {
    try {
      const abi = JSON.parse(item.content || "[]") as AbiItem[];
      return Array.isArray(abi) ? abi : null;
    } catch {
      return null;
    }
  }, [item.content]);

  if (parsed === null) {
    return (
      <div>
        <p className="error">The ABI is not valid JSON. Raw content:</p>
        <pre className="abi-raw">{item.content}</pre>
      </div>
    );
  }

  const functions = parsed.filter((e) => e.type === "function");
  const events = parsed.filter((e) => e.type === "event");
  const errors = parsed.filter((e) => e.type === "error");
  const constructor = parsed.find((e) => e.type === "constructor");

  return (
    <div className="abi-detail">
      <div className="abi-head">
        <strong>{item.contractName}</strong>
        <span className="muted">
          {functions.length} functions · {events.length} events{errors.length ? ` · ${errors.length} errors` : ""}
        </span>
      </div>

      {constructor && (
        <AbiSection title="Constructor">
          <div className="abi-entry">
            <code className="abi-sig">
              constructor({(constructor.inputs ?? []).map(formatParam).join(", ")})
            </code>
            {constructor.stateMutability === "payable" && <span className="badge badge-warn">payable</span>}
          </div>
        </AbiSection>
      )}

      {functions.length > 0 && (
        <AbiSection title={`Functions (${functions.length})`}>
          {functions.map((f, i) => (
            <div className="abi-entry" key={`${f.name}-${i}`}>
              <code className="abi-sig">
                <span className="abi-name">{f.name}</span>({(f.inputs ?? []).map(formatParam).join(", ")})
                {f.outputs && f.outputs.length > 0 && (
                  <span className="abi-returns"> → ({f.outputs.map(formatParam).join(", ")})</span>
                )}
              </code>
              <span className={`badge ${mutabilityTone(f.stateMutability)}`}>{f.stateMutability ?? "nonpayable"}</span>
            </div>
          ))}
        </AbiSection>
      )}

      {events.length > 0 && (
        <AbiSection title={`Events (${events.length})`}>
          {events.map((e, i) => (
            <div className="abi-entry" key={`${e.name}-${i}`}>
              <code className="abi-sig">
                <span className="abi-name">{e.name}</span>({(e.inputs ?? []).map(formatParam).join(", ")})
              </code>
              {e.anonymous && <span className="badge badge-muted">anonymous</span>}
            </div>
          ))}
        </AbiSection>
      )}

      {errors.length > 0 && (
        <AbiSection title={`Errors (${errors.length})`}>
          {errors.map((e, i) => (
            <div className="abi-entry" key={`${e.name}-${i}`}>
              <code className="abi-sig">
                <span className="abi-name">{e.name}</span>({(e.inputs ?? []).map(formatParam).join(", ")})
              </code>
            </div>
          ))}
        </AbiSection>
      )}

      {functions.length === 0 && events.length === 0 && errors.length === 0 && !constructor && (
        <p className="muted">This ABI has no functions, events or errors.</p>
      )}
    </div>
  );
}

function AbiSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="abi-section">
      <div className="abi-section-title">{title}</div>
      {children}
    </div>
  );
}
