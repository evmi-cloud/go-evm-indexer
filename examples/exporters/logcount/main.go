// Example EVMI exporter plugin.
//
// Build it into a loadable plugin with the SAME Go toolchain and module
// versions the EVMI server was built with:
//
//	go build -buildmode=plugin -o logcount.so ./examples/exporters/logcount
//
// Then point an EvmiExporter's PluginLocalPath at logcount.so (or its source
// directory, which the server will build automatically).
package main

import (
	"encoding/json"
	"fmt"

	exporter "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
)

// pluginConfig is decoded from the exporter's PluginConfig JSON.
type pluginConfig struct {
	// LogEvery controls how often a running total is printed.
	LogEvery uint64 `json:"logEvery"`
}

type logCounter struct {
	name     string
	cfg      pluginConfig
	total    uint64
	byEvent  map[string]uint64
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
	fmt.Printf("[%s] init pipeline=%d chain=%d\n", e.name, ctx.PipelineId, ctx.ChainId)
	return nil
}

func (e *logCounter) NewLogEvent(log exporter.LogEvent) error {
	e.total++
	name := log.EventName
	if name == "" {
		name = "<undecoded>"
	}
	e.byEvent[name]++

	if e.total%e.cfg.LogEvery == 0 {
		fmt.Printf("[%s] block=%d total=%d event=%s\n", e.name, log.BlockNumber, e.total, name)
	}
	return nil
}

func (e *logCounter) Close() error {
	fmt.Printf("[%s] closing, %d logs seen: %v\n", e.name, e.total, e.byEvent)
	return nil
}

// New is the symbol the EVMI server looks up to instantiate the plugin.
func New() exporter.Exporter { return &logCounter{} }

// main is required for -buildmode=plugin (package main) but is never executed.
func main() {}
