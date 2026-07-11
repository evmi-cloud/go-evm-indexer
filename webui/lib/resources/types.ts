// Shared model + helpers for the declarative resource definitions. Each entity
// lives in its own file (blockchains.ts, abis.ts, …) and is assembled in
// index.ts.

export type FieldType =
  | "text"
  | "password"
  | "textarea"
  | "number"
  | "bigint"
  | "checkbox"
  | "select"
  | "pluginConfig"
  | "keyedConfig"
  | "topicFilters";

// One declared plugin config parameter (mirrors pkg/exporter.ConfigField).
export type PluginConfigField = {
  name: string;
  type: "string" | "number" | "bool";
  required?: boolean;
  description?: string;
  default?: string;
};

// One parameter of a "keyedConfig" field (string values → config JSON).
export type ConfigParam = {
  name: string;
  label?: string;
  type?: "text" | "password" | "number";
  placeholder?: string;
  help?: string;
  required?: boolean;
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
  // For a "select" whose options depend on other form values (loaded async and
  // re-loaded whenever any field named in `depends` changes) — e.g. the events
  // of a selected ABI. Receives the current form values.
  loadOptions?: (values: FormValues) => Promise<Option[]>;
  depends?: string[];
  // Show this field only when the predicate (given the current form values)
  // returns true — e.g. fields relevant only to a certain source type.
  showIf?: (values: FormValues) => boolean;
  // For type "pluginConfig"/"keyedConfig": the name of the sibling field whose
  // value drives this config form (plugin id, or a type discriminator).
  dependsOn?: string;
  // For type "keyedConfig": config params keyed by the dependsOn field's value.
  schemas?: Record<string, ConfigParam[]>;
};

export type Tone = "ok" | "warn" | "danger" | "muted" | "neutral";
import type { ComponentType } from "react";

export type Column<T> = { label: string; get: (item: T) => string; tone?: (item: T) => Tone; mono?: boolean };
export type RowAction<T> = { label: string; run: (item: T) => Promise<void> };
// Optional per-row read-only detail view, opened via a "Details" button.
export type DetailComponent<T> = ComponentType<{ item: T }>;

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
  // Returning a string surfaces it as a one-time secret to copy (e.g. a token).
  create: (values: FormValues) => Promise<string | void>;
  // Omit update to make the resource create-only (hides the Edit action).
  update?: (id: number, values: FormValues) => Promise<void>;
  remove: (id: number) => Promise<void>;
  toForm?: (item: T) => FormValues;
  actions?: RowAction<T>[];
  stream?: StreamSubscribe<T>;
  // Read-only detail view rendered in a modal via a "Details" button.
  detail?: DetailComponent<T>;
  // Only shown to admin users (grouped under "Admin" in the nav).
  adminOnly?: boolean;
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
