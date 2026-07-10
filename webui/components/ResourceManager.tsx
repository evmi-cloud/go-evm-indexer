"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { ConnectError } from "@connectrpc/connect";
import { client } from "@/lib/client";
import type { Field, FormValues, Option, PluginConfigField, Resource } from "@/lib/resources";

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
    setValues(item ? { ...defaults(resource.fields), ...resource.toForm(item) } : defaults(resource.fields));
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
      if (editing) await resource.update(resource.idOf(editing), values);
      else await resource.create(values);
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
                        <button className="secondary small" onClick={() => openForm(item)}>
                          Edit
                        </button>
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
            {resource.fields.map((f) =>
              f.type === "pluginConfig" ? (
                <PluginConfigInput
                  key={f.name}
                  field={f}
                  value={String(values[f.name] ?? "")}
                  pluginId={String(values[f.dependsOn ?? "pluginId"] ?? "")}
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
          type={field.type === "number" || field.type === "bigint" ? "number" : "text"}
          value={String(value ?? "")}
          placeholder={field.placeholder}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {field.help && <p className="field-help">{field.help}</p>}
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
