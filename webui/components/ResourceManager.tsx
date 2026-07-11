"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { client } from "@/lib/client";
import type { ConfigParam, Field, FormValues, Option, PluginConfigField, Resource } from "@/lib/resources";
import { abiEventIndexedArgs, type IndexedArg } from "@/lib/resources/options";

function errorMessage(err: unknown): string {
  return err instanceof ConnectError ? err.message : err instanceof Error ? err.message : "error";
}

function defaults(fields: Field[]): FormValues {
  const v: FormValues = {};
  for (const f of fields) v[f.name] = f.type === "checkbox" ? false : "";
  return v;
}

export default function ResourceManager<T>({ resource }: { resource: Resource<T> }) {
  const [items, setItems] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  // form state: editing === undefined -> closed, null -> create, T -> edit
  const [editing, setEditing] = useState<T | null | undefined>(undefined);
  const [values, setValues] = useState<FormValues>({});
  const [options, setOptions] = useState<Record<string, Option[]>>({});
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const [detailItem, setDetailItem] = useState<T | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setItems(await resource.list());
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }, [resource]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Live updates: merge streamed items into the list by id (append if new).
  useEffect(() => {
    if (!resource.stream) return;
    const controller = new AbortController();
    resource.stream((item) => {
      setItems((prev) => {
        const id = resource.idOf(item);
        const idx = prev.findIndex((x) => resource.idOf(x) === id);
        if (idx === -1) return [...prev, item];
        const next = [...prev];
        next[idx] = item;
        return next;
      });
    }, controller.signal);
    return () => controller.abort();
  }, [resource]);

  async function openForm(item: T | null) {
    setFormError(null);
    setValues(item ? { ...defaults(resource.fields), ...(resource.toForm?.(item) ?? {}) } : defaults(resource.fields));
    setEditing(item);

    // Load relation dropdowns and default empty selects to the first option.
    const loaded: Record<string, Option[]> = {};
    await Promise.all(
      resource.fields
        .filter((f) => f.optionsFrom)
        .map(async (f) => {
          try {
            loaded[f.name] = await f.optionsFrom!();
          } catch {
            loaded[f.name] = [];
          }
        }),
    );
    setOptions(loaded);
    if (!item) {
      setValues((prev) => {
        const next = { ...prev };
        for (const f of resource.fields) {
          if (f.optionsFrom && !next[f.name] && loaded[f.name]?.[0]) next[f.name] = loaded[f.name][0].value;
          if (f.options && !next[f.name] && f.options[0]) next[f.name] = f.options[0].value;
        }
        return next;
      });
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setFormError(null);
    try {
      // Drop values of hidden (not-applicable) fields so they aren't persisted.
      const effective: FormValues = {};
      for (const f of resource.fields) {
        const visible = !f.showIf || f.showIf(values);
        effective[f.name] = visible ? values[f.name] : f.type === "checkbox" ? false : "";
      }

      if (editing) {
        if (resource.update) await resource.update(resource.idOf(editing), effective);
      } else {
        const result = await resource.create(effective);
        if (typeof result === "string") setSecret(result);
      }
      setEditing(undefined);
      await refresh();
    } catch (err) {
      setFormError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  async function remove(item: T) {
    if (!confirm(`Delete this ${resource.singular}?`)) return;
    setBusy(`del-${resource.idOf(item)}`);
    try {
      await resource.remove(resource.idOf(item));
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function runAction(label: string, run: (i: T) => Promise<void>, item: T) {
    setBusy(`${label}-${resource.idOf(item)}`);
    setError(null);
    try {
      await run(item);
      await refresh();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h2>
            {resource.title}
            {!loading && <span className="count">{items.length}</span>}
            {resource.stream && <span className="live" title="Live updates">● live</span>}
          </h2>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <button className="secondary" onClick={refresh} disabled={loading}>
            Refresh
          </button>
          <button onClick={() => openForm(null)}>+ New {resource.singular}</button>
        </div>
      </div>

      {error && <div className="error banner">{error}</div>}

      {secret && (
        <div className="secret-banner">
          <div>
            <strong>Copy it now — it won&apos;t be shown again:</strong>
            <div>
              <code>{secret}</code>
            </div>
          </div>
          <button className="secondary small" onClick={() => setSecret(null)}>
            Dismiss
          </button>
        </div>
      )}

      {loading ? (
        <div className="empty muted">Loading…</div>
      ) : items.length === 0 ? (
        <div className="empty">
          <p className="muted">No {resource.title.toLowerCase()} yet.</p>
          <button onClick={() => openForm(null)}>Create the first {resource.singular}</button>
        </div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                {resource.columns.map((c) => (
                  <th key={c.label}>{c.label}</th>
                ))}
                <th style={{ textAlign: "right" }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const id = resource.idOf(item);
                return (
                  <tr key={id}>
                    {resource.columns.map((c) => {
                      const text = c.get(item);
                      return (
                        <td key={c.label} title={text} className={c.mono ? "mono" : undefined}>
                          {c.tone ? <span className={`badge badge-${c.tone(item)}`}>{text}</span> : text}
                        </td>
                      );
                    })}
                    <td>
                      <div className="actions">
                        {resource.actions?.map((a) => (
                          <button
                            key={a.label}
                            className="secondary small"
                            disabled={busy === `${a.label}-${id}`}
                            onClick={() => runAction(a.label, a.run, item)}
                          >
                            {busy === `${a.label}-${id}` ? "…" : a.label}
                          </button>
                        ))}
                        {resource.detail && (
                          <button className="secondary small" onClick={() => setDetailItem(item)}>
                            Details
                          </button>
                        )}
                        {resource.update && (
                          <button className="secondary small" onClick={() => openForm(item)}>
                            Edit
                          </button>
                        )}
                        <button className="danger small" disabled={busy === `del-${id}`} onClick={() => remove(item)}>
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {editing !== undefined && (
        <div className="modal-backdrop" onClick={() => setEditing(undefined)}>
          <form className="modal panel" onClick={(e) => e.stopPropagation()} onSubmit={submit}>
            <h3>
              {editing ? "Edit" : "New"} {resource.singular}
            </h3>
            {resource.fields
              .filter((f) => !f.showIf || f.showIf(values))
              .map((f) =>
              f.type === "pluginConfig" ? (
                <PluginConfigInput
                  key={f.name}
                  field={f}
                  value={String(values[f.name] ?? "")}
                  pluginId={String(values[f.dependsOn ?? "pluginId"] ?? "")}
                  onChange={(val) => setValues((prev) => ({ ...prev, [f.name]: val }))}
                />
              ) : f.type === "keyedConfig" ? (
                <KeyedConfigInput
                  key={f.name}
                  field={f}
                  value={String(values[f.name] ?? "")}
                  typeKey={String(values[f.dependsOn ?? "type"] ?? "")}
                  onChange={(val) => setValues((prev) => ({ ...prev, [f.name]: val }))}
                />
              ) : f.type === "topicFilters" ? (
                <TopicFiltersInput
                  key={f.name}
                  field={f}
                  value={String(values[f.name] ?? "")}
                  abiId={String(values.evmJsonAbiId ?? "")}
                  topic0={String(values.topic0 ?? "")}
                  onChange={(val) => setValues((prev) => ({ ...prev, [f.name]: val }))}
                />
              ) : f.type === "select" && f.loadOptions ? (
                <DynamicSelectInput
                  key={f.name}
                  field={f}
                  value={values[f.name]}
                  values={values}
                  onChange={(val) => setValues((prev) => ({ ...prev, [f.name]: val }))}
                />
              ) : (
                <FieldInput
                  key={f.name}
                  field={f}
                  value={values[f.name]}
                  options={f.options ?? options[f.name] ?? []}
                  onChange={(val) => setValues((prev) => ({ ...prev, [f.name]: val }))}
                />
              ),
            )}
            {formError && <div className="error">{formError}</div>}
            <div className="row" style={{ marginTop: 12 }}>
              <button type="button" className="secondary" onClick={() => setEditing(undefined)}>
                Cancel
              </button>
              <button type="submit" disabled={saving}>
                {saving ? "Saving…" : "Save"}
              </button>
            </div>
          </form>
        </div>
      )}

      {detailItem !== null && resource.detail && (
        <div className="modal-backdrop" onClick={() => setDetailItem(null)}>
          <div className="modal panel modal-wide" onClick={(e) => e.stopPropagation()}>
            <div className="row" style={{ justifyContent: "space-between", marginBottom: 12 }}>
              <h3 style={{ margin: 0 }}>Details</h3>
              <button className="secondary small" onClick={() => setDetailItem(null)}>
                Close
              </button>
            </div>
            <resource.detail item={detailItem} />
          </div>
        </div>
      )}
    </div>
  );
}

function FieldInput({
  field,
  value,
  options,
  onChange,
}: {
  field: Field;
  value: string | boolean;
  options: Option[];
  onChange: (v: string | boolean) => void;
}) {
  if (field.type === "checkbox") {
    return (
      <label className="checkbox">
        <input type="checkbox" checked={Boolean(value)} onChange={(e) => onChange(e.target.checked)} />
        {field.label}
      </label>
    );
  }

  return (
    <div>
      <label>{field.label}</label>
      {field.type === "textarea" ? (
        <textarea
          value={String(value ?? "")}
          placeholder={field.placeholder}
          rows={4}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : field.type === "select" ? (
        <select value={String(value ?? "")} onChange={(e) => onChange(e.target.value)}>
          <option value="">—</option>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      ) : (
        <input
          type={
            field.type === "number" || field.type === "bigint"
              ? "number"
              : field.type === "password"
                ? "password"
                : "text"
          }
          value={String(value ?? "")}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {field.help && <p className="field-help">{field.help}</p>}
    </div>
  );
}

// DynamicSelectInput renders a select whose options are loaded async from the
// current form values (field.loadOptions), re-loading whenever any field named
// in field.depends changes — e.g. the events of a selected ABI.
function DynamicSelectInput({
  field,
  value,
  values,
  onChange,
}: {
  field: Field;
  value: string | boolean;
  values: FormValues;
  onChange: (v: string | boolean) => void;
}) {
  const [options, setOptions] = useState<Option[]>([]);
  const depKey = (field.depends ?? []).map((d) => String(values[d] ?? "")).join("|");

  useEffect(() => {
    let cancelled = false;
    field
      .loadOptions!(values)
      .then((opts) => !cancelled && setOptions(opts))
      .catch(() => !cancelled && setOptions([]));
    return () => {
      cancelled = true;
    };
    // Re-run only when the depended-on values change; `values` is read inside.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [depKey]);

  return <FieldInput field={field} value={value} options={options} onChange={onChange} />;
}

// Encode a human-friendly indexed-argument value into its 32-byte topic hash
// (what topics[1..] are matched against). Empty string = wildcard.
function encodeTopic(type: string, raw: string): string {
  const v = raw.trim();
  if (v === "") return "";
  if (type === "address") {
    return "0x" + v.replace(/^0x/i, "").toLowerCase().padStart(64, "0");
  }
  if (/^u?int\d*$/.test(type) || type === "bool") {
    let n: bigint;
    try {
      n = type === "bool" ? BigInt(v === "true" || v === "1" ? 1 : 0) : BigInt(v);
    } catch {
      return v.toLowerCase().startsWith("0x") ? v : "0x" + v;
    }
    if (n < BigInt(0)) n = (BigInt(1) << BigInt(256)) + n; // two's complement for signed ints
    return "0x" + n.toString(16).padStart(64, "0");
  }
  if (/^bytes\d+$/.test(type)) {
    // Fixed bytes are left-aligned (right-padded) within the 32-byte topic.
    return "0x" + v.replace(/^0x/i, "").padEnd(64, "0").slice(0, 64);
  }
  // Dynamic types (string, bytes, arrays, tuples): the topic is keccak256(value)
  // and cannot be reconstructed here — accept a raw 32-byte hash.
  return v.toLowerCase().startsWith("0x") ? v : "0x" + v;
}

// Best-effort inverse of encodeTopic, to seed the friendly inputs when editing.
function decodeTopic(type: string, stored: string): string {
  const v = (stored || "").trim();
  if (v === "") return "";
  const hex = v.replace(/^0x/i, "");
  if (hex.length !== 64) return v; // not a full topic — show as-is
  if (type === "address") return "0x" + hex.slice(24);
  if (/^u?int\d*$/.test(type)) {
    try {
      return BigInt("0x" + hex).toString(10);
    } catch {
      return v;
    }
  }
  if (type === "bool") {
    try {
      return BigInt("0x" + hex) ? "true" : "false";
    } catch {
      return v;
    }
  }
  return v; // bytesN / dynamic: show the raw topic
}

function topicPlaceholder(type: string): string {
  if (type === "address") return "0x… (20-byte address, blank = any)";
  if (/^u?int\d*$/.test(type)) return "decimal or 0x… (blank = any)";
  if (type === "bool") return "true / false (blank = any)";
  if (/^bytes\d+$/.test(type)) return "0x… (blank = any)";
  return "0x… 32-byte topic (blank = any)";
}

// TopicFiltersInput renders one filter input per indexed argument of the event
// selected as topic0, in declaration order (mapping to topics[1..]). Each value
// is encoded to its 32-byte topic; a blank field is a wildcard at that position.
// The persisted value is the newline-joined list of encoded topics.
function TopicFiltersInput({
  field,
  value,
  abiId,
  topic0,
  onChange,
}: {
  field: Field;
  value: string;
  abiId: string;
  topic0: string;
  onChange: (v: string) => void;
}) {
  // undefined = loading, [] = none/no event, [...] = the event's indexed args
  const [args, setArgs] = useState<IndexedArg[] | undefined>(undefined);
  // Friendly (decoded) entry values, seeded from the stored list when the event changes.
  const [entries, setEntries] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;
    if (!abiId || !topic0) {
      setArgs([]);
      return;
    }
    setArgs(undefined);
    abiEventIndexedArgs(abiId, topic0)
      .then((a) => !cancelled && setArgs(a))
      .catch(() => !cancelled && setArgs([]));
    return () => {
      cancelled = true;
    };
  }, [abiId, topic0]);

  // Seed friendly inputs from the persisted topics whenever the arg set changes
  // (event switch / initial load). Not keyed on `value` so typing isn't clobbered.
  useEffect(() => {
    if (!args) return;
    const stored = value ? value.split("\n") : [];
    setEntries(args.map((a, i) => decodeTopic(a.type, stored[i] ?? "")));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [args]);

  function update(i: number, friendly: string) {
    if (!args) return;
    const next = [...entries];
    next[i] = friendly;
    setEntries(next);
    const encoded = args.map((a, j) => encodeTopic(a.type, next[j] ?? ""));
    // Trim trailing wildcards so we don't persist empty positions.
    while (encoded.length && encoded[encoded.length - 1] === "") encoded.pop();
    onChange(encoded.join("\n"));
  }

  return (
    <div>
      <label>{field.label}</label>
      {args === undefined ? (
        <p className="muted" style={{ fontSize: 13 }}>
          Loading event arguments…
        </p>
      ) : !topic0 ? (
        <p className="field-help">Select an event above to configure its indexed-argument filters.</p>
      ) : args.length === 0 ? (
        <p className="field-help">The selected event has no indexed arguments to filter on.</p>
      ) : (
        <div className="config-schema">
          {args.map((a, i) => (
            <div key={`${a.name}-${i}`}>
              <label>
                {a.name} <span className="muted">({a.type})</span>
              </label>
              <input
                type="text"
                value={entries[i] ?? ""}
                placeholder={topicPlaceholder(a.type)}
                onChange={(e) => update(i, e.target.value)}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// PluginConfigInput renders the config form for the selected plugin, driven by
// that plugin's declared schema. Falls back to a raw JSON textarea when the
// plugin declares no schema. The value is always a JSON string.
function PluginConfigInput({
  field,
  value,
  pluginId,
  onChange,
}: {
  field: Field;
  value: string;
  pluginId: string;
  onChange: (v: string) => void;
}) {
  // undefined = loading, null = no schema
  const [schema, setSchema] = useState<PluginConfigField[] | null | undefined>(undefined);

  useEffect(() => {
    if (!pluginId || pluginId === "0") {
      setSchema(null);
      return;
    }
    let cancelled = false;
    setSchema(undefined);
    client
      .getPlugin({ id: Number(pluginId) })
      .then((r) => {
        if (cancelled) return;
        const raw = r.plugin?.configSchemaJson;
        if (!raw) return setSchema(null);
        try {
          const parsed = JSON.parse(raw) as PluginConfigField[];
          setSchema(Array.isArray(parsed) && parsed.length ? parsed : null);
        } catch {
          setSchema(null);
        }
      })
      .catch(() => !cancelled && setSchema(null));
    return () => {
      cancelled = true;
    };
  }, [pluginId]);

  const config = useMemo<Record<string, unknown>>(() => {
    try {
      return JSON.parse(value || "{}");
    } catch {
      return {};
    }
  }, [value]);

  function setParam(name: string, v: unknown) {
    const next = { ...config };
    if (v === "" || v === undefined) delete next[name];
    else next[name] = v;
    onChange(JSON.stringify(next));
  }

  if (schema === undefined) {
    return (
      <div>
        <label>{field.label}</label>
        <p className="muted" style={{ fontSize: 13 }}>
          Loading plugin config…
        </p>
      </div>
    );
  }

  if (schema === null) {
    return (
      <div>
        <label>{field.label}</label>
        <textarea value={value} placeholder="{}" rows={4} onChange={(e) => onChange(e.target.value)} />
        <p className="field-help">This plugin declares no config schema — enter raw JSON.</p>
      </div>
    );
  }

  return (
    <div>
      <label>{field.label}</label>
      <div className="config-schema">
        {schema.map((p) =>
          p.type === "bool" ? (
            <label className="checkbox" key={p.name}>
              <input type="checkbox" checked={Boolean(config[p.name])} onChange={(e) => setParam(p.name, e.target.checked)} />
              {p.name}
              {p.required && " *"}
            </label>
          ) : (
            <div key={p.name}>
              <label>
                {p.name}
                {p.required && " *"}
              </label>
              <input
                type={p.type === "number" ? "number" : "text"}
                value={config[p.name] === undefined ? "" : String(config[p.name])}
                placeholder={p.default}
                onChange={(e) =>
                  setParam(p.name, p.type === "number" ? (e.target.value === "" ? "" : Number(e.target.value)) : e.target.value)
                }
              />
              {p.description && <p className="field-help">{p.description}</p>}
            </div>
          ),
        )}
      </div>
    </div>
  );
}

// KeyedConfigInput renders a typed config form whose fields depend on the value
// of a sibling discriminator field (e.g. the log-store type), from a static
// schema map. Values are strings, serialized to a JSON object.
function KeyedConfigInput({
  field,
  value,
  typeKey,
  onChange,
}: {
  field: Field;
  value: string;
  typeKey: string;
  onChange: (v: string) => void;
}) {
  const params: ConfigParam[] = field.schemas?.[typeKey] ?? [];

  const config = useMemo<Record<string, string>>(() => {
    try {
      return JSON.parse(value || "{}");
    } catch {
      return {};
    }
  }, [value]);

  function setParam(name: string, v: string) {
    const next = { ...config };
    if (v === "") delete next[name];
    else next[name] = v;
    onChange(JSON.stringify(next));
  }

  if (params.length === 0) {
    return (
      <div>
        <label>{field.label}</label>
        <textarea value={value} placeholder="{}" rows={4} onChange={(e) => onChange(e.target.value)} />
        <p className="field-help">Enter raw JSON config.</p>
      </div>
    );
  }

  return (
    <div>
      <label>{field.label}</label>
      <div className="config-schema">
        {params.map((p) => (
          <div key={p.name}>
            <label>
              {p.label ?? p.name}
              {p.required && " *"}
            </label>
            <input
              type={p.type ?? "text"}
              value={config[p.name] ?? ""}
              placeholder={p.placeholder}
              onChange={(e) => setParam(p.name, e.target.value)}
            />
            {p.help && <p className="field-help">{p.help}</p>}
          </div>
        ))}
      </div>
    </div>
  );
}
