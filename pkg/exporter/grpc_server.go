package exporter

import (
	"context"

	exporterproto "github.com/evmi-cloud/go-evm-indexer/pkg/exporter/proto"
)

// grpcServer adapts a plugin author's Exporter implementation to the gRPC
// service EVMI calls. It runs inside the plugin process.
//
// Errors are returned as plain gRPC errors: the host turns the status message
// back into an error, so a plugin's error text reaches the EVMI log and the
// exporter's status verbatim.
type grpcServer struct {
	exporterproto.UnimplementedExporterPluginServiceServer

	impl Exporter
}

func (s *grpcServer) Name(ctx context.Context, req *exporterproto.NameRequest) (*exporterproto.NameResponse, error) {
	return &exporterproto.NameResponse{Name: s.impl.Name()}, nil
}

// ConfigSchema reports the optional Configurable interface. A plugin that does
// not implement it answers implemented=false, which EVMI records as "no schema"
// (any config accepted).
func (s *grpcServer) ConfigSchema(ctx context.Context, req *exporterproto.ConfigSchemaRequest) (*exporterproto.ConfigSchemaResponse, error) {
	configurable, ok := s.impl.(Configurable)
	if !ok {
		return &exporterproto.ConfigSchemaResponse{Implemented: false}, nil
	}

	fields := configurable.ConfigSchema()
	out := make([]*exporterproto.ConfigField, 0, len(fields))
	for _, f := range fields {
		out = append(out, &exporterproto.ConfigField{
			Name:         f.Name,
			Type:         string(f.Type),
			Required:     f.Required,
			Description:  f.Description,
			DefaultValue: f.Default,
		})
	}
	return &exporterproto.ConfigSchemaResponse{Implemented: true, Fields: out}, nil
}

func (s *grpcServer) Init(ctx context.Context, req *exporterproto.InitRequest) (*exporterproto.InitResponse, error) {
	err := s.impl.Init(Context{
		ExporterName: req.ExporterName,
		PipelineId:   req.PipelineId,
		ChainId:      req.ChainId,
		Config:       req.Config,
	})
	if err != nil {
		return nil, err
	}
	return &exporterproto.InitResponse{}, nil
}

func (s *grpcServer) NewLogEvent(ctx context.Context, req *exporterproto.NewLogEventRequest) (*exporterproto.NewLogEventResponse, error) {
	if err := s.impl.NewLogEvent(logEventFromProto(req.Log)); err != nil {
		return nil, err
	}
	return &exporterproto.NewLogEventResponse{}, nil
}

func (s *grpcServer) Close(ctx context.Context, req *exporterproto.CloseRequest) (*exporterproto.CloseResponse, error) {
	if err := s.impl.Close(); err != nil {
		return nil, err
	}
	return &exporterproto.CloseResponse{}, nil
}

// logEventFromProto decodes a wire log back into the author-facing struct.
func logEventFromProto(l *exporterproto.LogEvent) LogEvent {
	if l == nil {
		return LogEvent{}
	}
	return LogEvent{
		Id:               l.Id,
		SourceId:         uint(l.SourceId),
		ChainId:          l.ChainId,
		Address:          l.Address,
		Topics:           l.Topics,
		Data:             l.Data,
		BlockNumber:      l.BlockNumber,
		BlockTimestamp:   l.BlockTimestamp,
		TransactionHash:  l.TransactionHash,
		TransactionFrom:  l.TransactionFrom,
		TransactionIndex: l.TransactionIndex,
		BlockHash:        l.BlockHash,
		LogIndex:         l.LogIndex,
		Removed:          l.Removed,
		ContractName:     l.ContractName,
		EventName:        l.EventName,
		Args:             l.Args,
	}
}

// logEventToProto encodes a log for the wire (host side).
func logEventToProto(l LogEvent) *exporterproto.LogEvent {
	return &exporterproto.LogEvent{
		Id:               l.Id,
		SourceId:         uint64(l.SourceId),
		ChainId:          l.ChainId,
		Address:          l.Address,
		Topics:           l.Topics,
		Data:             l.Data,
		BlockNumber:      l.BlockNumber,
		BlockTimestamp:   l.BlockTimestamp,
		TransactionHash:  l.TransactionHash,
		TransactionFrom:  l.TransactionFrom,
		TransactionIndex: l.TransactionIndex,
		BlockHash:        l.BlockHash,
		LogIndex:         l.LogIndex,
		Removed:          l.Removed,
		ContractName:     l.ContractName,
		EventName:        l.EventName,
		Args:             l.Args,
	}
}
