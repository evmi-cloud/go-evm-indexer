package exporter

import (
	"context"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// PluginName is the key an exporter plugin is served and dispensed under. A
// plugin binary serves exactly one exporter, so there is only ever this one.
const PluginName = "exporter"

// Handshake is the hashicorp/go-plugin handshake both sides must agree on. The
// magic cookie makes a plugin binary refuse to do anything useful when run
// directly from a shell instead of being launched by EVMI; ProtocolVersion is
// bumped when the wire protocol changes incompatibly, which makes the host
// reject plugins built against an older SDK with a clear error instead of
// misbehaving at runtime.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "EVMI_EXPORTER_PLUGIN",
	MagicCookieValue: "b1f1a0c6-evmi-exporter",
}

// ExporterPlugin is the go-plugin implementation for the exporter interface. The
// plugin process sets Impl and serves it; the host leaves Impl nil and only uses
// it to build a client.
type ExporterPlugin struct {
	// NetRPCUnsupportedPlugin makes the net/rpc protocol a clear error: this
	// plugin type speaks gRPC only.
	goplugin.NetRPCUnsupportedPlugin

	Impl Exporter
}

var _ goplugin.GRPCPlugin = (*ExporterPlugin)(nil)

// GRPCServer registers the plugin-side implementation (plugin process).
func (p *ExporterPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	exporterproto.RegisterExporterPluginServiceServer(s, &grpcServer{impl: p.Impl})
	return nil
}

// GRPCClient returns the host-side view of a running plugin (EVMI process). The
// concrete type is *GRPCClient, which implements Exporter.
func (p *ExporterPlugin) GRPCClient(ctx context.Context, broker *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &GRPCClient{client: exporterproto.NewExporterPluginServiceClient(c)}, nil
}

// PluginMap is the plugin set served by a plugin binary / dispensed by the host.
// Pass a nil impl on the host side.
func PluginMap(impl Exporter) map[string]goplugin.Plugin {
	return map[string]goplugin.Plugin{
		PluginName: &ExporterPlugin{Impl: impl},
	}
}

// Serve runs the exporter as a plugin process. It is the only thing a plugin's
// main() needs to do:
//
//	func main() { exporter.Serve(&myExporter{}) }
//
// It blocks until the EVMI server closes the connection, then returns (at which
// point main should exit). Never write to stdout from a plugin: stdout is the
// handshake/gRPC channel. Use stderr (or the standard library logger, which
// defaults to stderr) — EVMI captures it and forwards it to its own log.
func Serve(impl Exporter) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}
