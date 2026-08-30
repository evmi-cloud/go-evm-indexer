// Example EVMI exporter plugin.
//
// An exporter plugin is an ordinary Go program: EVMI launches it as a subprocess
// and talks to it over gRPC (hashicorp/go-plugin). Build it like anything else:
//
//	go build -o logcount ./examples/exporters/logcount
//
// In practice you don't build it by hand: create a Plugin pointing at the git
// repository, with Path set to the directory holding the plugin's `main` package
// (empty for a repo that holds a single plugin at its root), and EVMI clones and
// builds it on install. Then point an EvmiExporter at that plugin. A repo hosting
// several plugins can list them in an evmi-plugins.json catalog — as this one
// does at its root.
package main

import (
	"encoding/json"
	"fmt"
	"log"

	exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

// pluginConfig is decoded from the exporter's PluginConfig JSON.
type pluginConfig struct {
	// LogEvery controls how often a running total is printed.
	LogEvery uint64 `json:"logEvery"`
}

type logCounter struct {
	name    string
	cfg     pluginConfig
	total   uint64
	byEvent map[string]uint64
}

func (e *logCounter) Name() string { return "logcount" }

// ConfigSchema declares the config parameters EVMI validates exporter configs
// against (and renders a form for in the UI). Implementing exporter.Configurable
// is optional.
func (e *logCounter) ConfigSchema() []exporter.ConfigField {
	return []exporter.ConfigField{
		{
			Name:        "logEvery",
			Type:        exporter.NumberField,
			Required:    false,
			Description: "Print a running total every N logs",
			Default:     "100",
		},
	}
}

func (e *logCounter) Init(ctx exporter.Context) error {
	e.name = ctx.ExporterName
	e.byEvent = map[string]uint64{}
	if len(ctx.Config) > 0 {
		if err := json.Unmarshal(ctx.Config, &e.cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}
	}
	if e.cfg.LogEvery == 0 {
		e.cfg.LogEvery = 100
	}
	log.Printf("[%s] init pipeline=%d chain=%d", e.name, ctx.PipelineId, ctx.ChainId)
	return nil
}

func (e *logCounter) NewLogEvent(l exporter.LogEvent) error {
	e.total++
	name := l.EventName
	if name == "" {
		name = "<undecoded>"
	}
	e.byEvent[name]++

	if e.total%e.cfg.LogEvery == 0 {
		log.Printf("[%s] block=%d total=%d event=%s", e.name, l.BlockNumber, e.total, name)
	}
	return nil
}

func (e *logCounter) Close() error {
	log.Printf("[%s] closing, %d logs seen: %v", e.name, e.total, e.byEvent)
	return nil
}

// main hands the implementation to the SDK, which serves it to EVMI and blocks
// until the server disconnects.
//
// Note the logging: everything goes through the standard logger (stderr), which
// EVMI captures and forwards to its own log. NEVER print to stdout from a plugin
// — stdout is the go-plugin handshake and gRPC channel.
func main() {
	exporter.Serve(&logCounter{})
}
