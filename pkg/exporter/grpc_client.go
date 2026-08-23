package exporter

import (
	"context"
	"errors"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
	"google.golang.org/grpc/status"
)

// GRPCClient is the host-side handle on a running plugin process: it implements
// Exporter by forwarding each call over gRPC. EVMI never constructs it directly
// — it comes out of dispensing the plugin (see internal/exporter/loader.go).
type GRPCClient struct {
	client exporterproto.ExporterPluginServiceClient
}

var _ Exporter = (*GRPCClient)(nil)

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
	})
	return pluginError(err)
}

func (c *GRPCClient) NewLogEvent(log LogEvent) error {
	_, err := c.client.NewLogEvent(context.Background(), &exporterproto.NewLogEventRequest{
		Log: logEventToProto(log),
	})
	return pluginError(err)
}

func (c *GRPCClient) Close() error {
	_, err := c.client.Close(context.Background(), &exporterproto.CloseRequest{})
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
