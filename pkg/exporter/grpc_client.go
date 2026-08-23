package exporter

import (
	"context"
	"errors"
	"sync"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// GRPCClient is the host-side handle on a running plugin process: it implements
// Exporter by forwarding each call over gRPC. EVMI never constructs it directly
// — it comes out of dispensing the plugin (see internal/exporter/loader.go).
//
// It also runs the reverse direction: when a Host is installed with SetHost, Init
// stands up an ExporterHostService on a go-plugin broker connection and tells the
// plugin which broker id to dial, so the plugin can call back into EVMI.
type GRPCClient struct {
	client exporterproto.ExporterPluginServiceClient
	broker *goplugin.GRPCBroker

	// mu guards the host API and the server serving it, which Init writes from
	// the broker's accept goroutine and Close reads.
	mu         sync.Mutex
	host       Host
	hostServer *grpc.Server
}

var _ Exporter = (*GRPCClient)(nil)

// SetHost installs the EVMI functions this plugin may call back into. EVMI calls
// it after dispensing the plugin and before Init. Leaving it unset is valid and
// simply means the plugin sees a nil Context.Host.
func (c *GRPCClient) SetHost(h Host) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.host = h
}

// Name returns the plugin's identifier, or "" if the process is unreachable
// (Exporter.Name has no error return; a broken process surfaces on the next
// call that does).
func (c *GRPCClient) Name() string {
	resp, err := c.client.Name(context.Background(), &exporterproto.NameRequest{})
	if err != nil {
		return ""
	}
	return resp.Name
}

func (c *GRPCClient) Init(ctx Context) error {
	_, err := c.client.Init(context.Background(), &exporterproto.InitRequest{
		ExporterName: ctx.ExporterName,
		PipelineId:   ctx.PipelineId,
		ChainId:      ctx.ChainId,
		Config:       ctx.Config,
		HostBrokerId: c.serveHost(),
	})
	return pluginError(err)
}

// serveHost stands up the host service on a broker connection and returns the id
// the plugin dials to reach it. It returns 0 when no host API was installed,
// which the plugin reads as "this server exposes no callbacks".
//
// AcceptAndServe blocks until the plugin dials, so it runs in its own goroutine;
// the callback fires inside the plugin's Init. A plugin that never dials leaves
// that goroutine parked until the process is killed, which closes the broker and
// unblocks it.
func (c *GRPCClient) serveHost() uint32 {
	c.mu.Lock()
	host := c.host
	c.mu.Unlock()

	if host == nil || c.broker == nil {
		return 0
	}

	brokerID := c.broker.NextId()
	go c.broker.AcceptAndServe(brokerID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		exporterproto.RegisterExporterHostServiceServer(s, &hostServer{impl: host})
		c.mu.Lock()
		c.hostServer = s
		c.mu.Unlock()
		return s
	})
	return brokerID
}

func (c *GRPCClient) NewLogEvent(log LogEvent) error {
	_, err := c.client.NewLogEvent(context.Background(), &exporterproto.NewLogEventRequest{
		Log: logEventToProto(log),
	})
	return pluginError(err)
}

func (c *GRPCClient) Close() error {
	_, err := c.client.Close(context.Background(), &exporterproto.CloseRequest{})

	// Stop the host service only after the plugin's own Close has returned: a
	// plugin is allowed to call back while flushing, and tearing the service down
	// first would fail those calls.
	c.mu.Lock()
	srv := c.hostServer
	c.hostServer = nil
	c.mu.Unlock()
	if srv != nil {
		srv.Stop()
	}

	return pluginError(err)
}

// ConfigSchema fetches the plugin's declared config parameters. The second
// return reports whether the plugin implements Configurable at all — false means
// "no schema declared", which is different from a plugin declaring an empty one.
func (c *GRPCClient) ConfigSchema() ([]ConfigField, bool, error) {
	resp, err := c.client.ConfigSchema(context.Background(), &exporterproto.ConfigSchemaRequest{})
	if err != nil {
		return nil, false, pluginError(err)
	}
	if !resp.Implemented {
		return nil, false, nil
	}

	fields := make([]ConfigField, 0, len(resp.Fields))
	for _, f := range resp.Fields {
		fields = append(fields, ConfigField{
			Name:        f.Name,
			Type:        ConfigFieldType(f.Type),
			Required:    f.Required,
			Description: f.Description,
			Default:     f.DefaultValue,
		})
	}
	return fields, true, nil
}

// pluginError unwraps a gRPC status into a plain error carrying the plugin's own
// message, so "connection refused"-style transport noise isn't layered on top of
// what the plugin actually said.
func pluginError(err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return errors.New(st.Message())
	}
	return err
}
