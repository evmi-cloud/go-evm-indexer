package exporter

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pluginsdk "github.com/evmi-cloud/go-evm-indexer/pkg/exporter"
	"github.com/rs/zerolog"
)

// buildExamplePlugin compiles examples/exporters/logcount the same way
// InstallPlugin compiles a cloned repo — a plain `go build` — and returns the
// binary path.
func buildExamplePlugin(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a plugin binary; skipped with -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	out := filepath.Join(t.TempDir(), "logcount"+exeSuffix())
	cmd := exec.Command("go", "build", "-o", out, "./examples/exporters/logcount")
	cmd.Dir = filepath.Join("..", "..") // repo root
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example plugin: %v: %s", err, combined)
	}
	return out
}

// The full round trip: launch the plugin as a subprocess, hand it a config,
// deliver a log, and shut it down. This is the contract every installed plugin
// goes through, so it covers the handshake, the gRPC transport and both adapters.
func TestPluginProcessRoundTrip(t *testing.T) {
	bin := buildExamplePlugin(t)

	process, err := startPlugin(bin, "logcount", zerolog.Nop())
	if err != nil {
		t.Fatalf("startPlugin: %v", err)
	}
	defer process.Kill()

	plug := process.Exporter()
	if name := plug.Name(); name != "logcount" {
		t.Errorf("Name() = %q, want %q", name, "logcount")
	}

	if err := plug.Init(pluginsdk.Context{
		ExporterName: "test-exporter",
		PipelineId:   1,
		ChainId:      1,
		Config:       []byte(`{"logEvery": 1}`),
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Every LogEvent field must survive the wire encoding.
	sent := pluginsdk.LogEvent{
		Id:               "1:10:2",
		SourceId:         7,
		ChainId:          1,
		Address:          "0xabc",
		Topics:           []string{"0xtopic0", "0xtopic1"},
		Data:             "deadbeef",
		BlockNumber:      10,
		BlockTimestamp:   1700000000,
		TransactionHash:  "0xtx",
		TransactionFrom:  "0xfrom",
		TransactionIndex: 3,
		BlockHash:        "0xblock",
		LogIndex:         2,
		Removed:          false,
		ContractName:     "Token",
		EventName:        "Transfer",
		Args:             map[string]string{"from": "0x1", "to": "0x2"},
	}
	if err := plug.NewLogEvent(sent); err != nil {
		t.Fatalf("NewLogEvent: %v", err)
	}

	if err := plug.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// A plugin that declares a config schema (exporter.Configurable) reports it over
// the wire; this is what install records on the Plugin row.
func TestPluginProcessConfigSchema(t *testing.T) {
	bin := buildExamplePlugin(t)

	process, err := startPlugin(bin, "logcount", zerolog.Nop())
	if err != nil {
		t.Fatalf("startPlugin: %v", err)
	}
	defer process.Kill()

	fields, declared, err := process.ConfigSchema()
	if err != nil {
		t.Fatalf("ConfigSchema: %v", err)
	}
	if !declared {
		t.Fatal("declared = false, want true (logcount implements Configurable)")
	}
	if len(fields) != 1 || fields[0].Name != "logEvery" || fields[0].Type != pluginsdk.NumberField {
		t.Fatalf("schema not round-tripped: %+v", fields)
	}
	if fields[0].Default != "100" {
		t.Errorf("Default = %q, want %q", fields[0].Default, "100")
	}
}

// An error returned inside the plugin reaches the host as an error carrying the
// plugin's own message — the exporter surfaces it as its failure reason.
func TestPluginProcessPropagatesError(t *testing.T) {
	bin := buildExamplePlugin(t)

	process, err := startPlugin(bin, "logcount", zerolog.Nop())
	if err != nil {
		t.Fatalf("startPlugin: %v", err)
	}
	defer process.Kill()

	err = process.Exporter().Init(pluginsdk.Context{
		ExporterName: "test-exporter",
		Config:       []byte("{not json"),
	})
	if err == nil {
		t.Fatal("expected Init to fail on malformed config, got nil")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Errorf("error = %q, want it to carry the plugin's message", err.Error())
	}
}

// Launching something that is not a plugin fails cleanly instead of hanging or
// leaving a child process behind.
func TestStartPluginRejectsMissingBinary(t *testing.T) {
	if _, err := startPlugin(filepath.Join(t.TempDir(), "nope"), "nope", zerolog.Nop()); err == nil {
		t.Fatal("expected an error for a missing binary, got nil")
	}
}

// Exited is what the export loop polls to notice a plugin that died while idle.
// A nil process (the test doubles, which inject a plugin directly) must report
// false rather than panic.
func TestPluginProcessExited(t *testing.T) {
	var absent *pluginProcess
	if absent.Exited() {
		t.Error("nil process reported as exited")
	}

	bin := buildExamplePlugin(t)
	process, err := startPlugin(bin, "logcount", zerolog.Nop())
	if err != nil {
		t.Fatalf("startPlugin: %v", err)
	}
	if process.Exited() {
		t.Fatal("freshly started plugin reported as exited")
	}

	process.Kill()
	if !process.Exited() {
		t.Error("killed plugin not reported as exited")
	}
}
