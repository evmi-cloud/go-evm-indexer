// Shared model + helpers for the declarative resource definitions. Each entity
// lives in its own file (blockchains.ts, abis.ts, …) and is assembled in
// index.ts.

export type FieldType = "text" | "textarea" | "number" | "bigint" | "checkbox" | "select" | "pluginConfig";

// One declared plugin config parameter (mirrors pkg/exporter.ConfigField).
export type PluginConfigField = {
  name: string;
  type: "string" | "number" | "bool";
  required?: boolean;
  description?: string;
  default?: string;
};
export type Option = { value: string; label: string };
export type FormValues = Record<string, string | boolean>;

export type Field = {
  name: string;
  label: string;
  type: FieldType;
  required?: boolean;
  placeholder?: string;
  help?: string;
  options?: Option[];
  optionsFrom?: () => Promise<Option[]>;
  // For type "pluginConfig": the name of the sibling field holding the plugin id
  // whose schema drives this config form.
  dependsOn?: string;
};

export type Tone = "ok" | "warn" | "danger" | "muted" | "neutral";
export type Column<T> = { label: string; get: (item: T) => string; tone?: (item: T) => Tone; mono?: boolean };
export type RowAction<T> = { label: string; run: (item: T) => Promise<void> };

// Optional live updates: subscribe until `signal` aborts, calling `onUpdate` for
// each changed item (merged into the list by id).
export type StreamSubscribe<T> = (onUpdate: (item: T) => void, signal: AbortSignal) => void;

export type Resource<T> = {
  key: string;
  title: string;
  singular: string;
  fields: Field[];
  columns: Column<T>[];
  idOf: (item: T) => number;
  list: () => Promise<T[]>;
  create: (values: FormValues) => Promise<void>;
  update: (id: number, values: FormValues) => Promise<void>;
  remove: (id: number) => Promise<void>;
  toForm: (item: T) => FormValues;
  actions?: RowAction<T>[];
  stream?: StreamSubscribe<T>;
};

// Standard pagination for list calls.
export const PAGE = { pagination: { offset: 0, limit: 200 } };

// Form-value coercion (inputs are strings; checkboxes are booleans).
export const str = (v: FormValues, k: string) => String(v[k] ?? "");
export const num = (v: FormValues, k: string) => Number(v[k] || 0);
export const bool = (v: FormValues, k: string) => Boolean(v[k]);
export const big = (v: FormValues, k: string) => BigInt((v[k] as string) || "0");
export const optStr = (v: FormValues, k: string) => (str(v, k) ? str(v, k) : undefined);
export const optNum = (v: FormValues, k: string) => (str(v, k) === "" ? undefined : Number(v[k]));
export const splitList = (s: string) =>
  s
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);
