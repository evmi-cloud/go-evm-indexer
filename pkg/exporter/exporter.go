// Package exporter is the public SDK that custom EVMI exporter plugins import.
//
// An exporter plugin is a Go package built with `-buildmode=plugin` that the
// EVMI server loads in-process. The server calls NewLogEvent once for every log
// stored in a pipeline's log store, in ascending (block_number, log_index)
// order, and tracks how far each exporter has progressed so it resumes from the
// last committed block after a restart.
//
// A plugin MUST be `package main`, implement Exporter, and export a symbol named
// `New` with the signature `func() exporter.Exporter`. The EVMI server looks up
// that symbol to instantiate the plugin. See examples/exporters for a template.
package exporter

// LogEvent is a single decoded EVM log delivered to a plugin. It mirrors the
// data persisted in the log store. Fields under Metadata are only populated for
// sources indexed with an ABI (CONTRACT/TOPIC/FACTORY sources); FULL sources
// leave EventName/Args empty.
type LogEvent struct {
	// Id is the store-stable identifier "chainId:blockNumber:logIndex". Use it
	// as an idempotency key: delivery is at-least-once, so a plugin may see the
	// same Id twice after a crash/restart and must treat writes as upserts.
	Id string

	SourceId         uint
	ChainId          uint64
	Address          string
	Topics           []string
	Data             string
	BlockNumber      uint64
	TransactionHash  string
	TransactionFrom  string
	TransactionIndex uint64
	BlockHash        string
	LogIndex         uint64
	Removed          bool

	// Decoded metadata (empty for undecoded/FULL logs).
	ContractName string
	EventName    string
	Args         map[string]string
}

// Context is passed to Init once, when the exporter starts.
type Context struct {
	ExporterName string
	PipelineId   uint64
	ChainId      uint64
	// Config is the raw PluginConfig JSON configured for this exporter. The
	// plugin decodes it into its own struct.
	Config []byte
}

// Exporter is the contract a plugin implements.
type Exporter interface {
	// Name returns a human-readable identifier for logs/metrics.
	Name() string
	// Init is called once before any NewLogEvent call. Open DB connections and
	// decode Config here. Returning an error stops the exporter.
	Init(ctx Context) error
	// NewLogEvent is called once per stored log in block order. Returning an
	// error stops the exporter without advancing the sync cursor past the
	// current block, so processing resumes from this block on the next start.
	NewLogEvent(log LogEvent) error
	// Close is called when the exporter is stopped. Flush and release resources.
	Close() error
}

// Factory is the type of the exported `New` symbol the server looks up.
type Factory = func() Exporter

// ConfigFieldType enumerates the supported configuration parameter types. The
// value in the exporter's config JSON must be a JSON string / number / boolean
// respectively.
type ConfigFieldType string

const (
	StringField ConfigFieldType = "string"
	NumberField ConfigFieldType = "number"
	BoolField   ConfigFieldType = "bool"
)

// ConfigField describes one configuration parameter a plugin expects. EVMI
// extracts the schema at install time, and validates each exporter's config
// against it when the exporter is created or updated.
type ConfigField struct {
	Name        string          `json:"name"`
	Type        ConfigFieldType `json:"type"`
	Required    bool            `json:"required"`
	Description string          `json:"description,omitempty"`
	Default     string          `json:"default,omitempty"`
}

// Configurable is an optional interface. A plugin implementing it declares the
// configuration parameters it accepts; EVMI stores this schema on the Plugin and
// validates exporter configs against it. Plugins that do not implement it accept
// any config (no validation).
type Configurable interface {
	ConfigSchema() []ConfigField
}
