package grpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/evmi-cloud/go-evm-indexer/internal/exporter"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

var errPluginInUse = errors.New("plugin is referenced by one or more exporters")

// CreatePlugin implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreatePlugin(ctx context.Context, req *connect.Request[evm_indexerv1.CreatePluginRequest]) (*connect.Response[evm_indexerv1.CreatePluginResponse], error) {
	plugin := evmi_database.Plugin{
		Name:         req.Msg.Plugin.Name,
		Description:  req.Msg.Plugin.Description,
		GithubUrl:    req.Msg.Plugin.GithubUrl,
		RelativePath: req.Msg.Plugin.RelativePath,
		LocalPath:    req.Msg.Plugin.LocalPath,
		Status:       string(evmi_database.NotInstalledPluginStatus),
	}
	if result := e.db.Conn.Create(&plugin); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.CreatePluginResponse{Id: uint32(plugin.ID)}), nil
}

// GetPlugin implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetPlugin(ctx context.Context, req *connect.Request[evm_indexerv1.GetPluginRequest]) (*connect.Response[evm_indexerv1.GetPluginResponse], error) {
	var plugin evmi_database.Plugin
	if result := e.db.Conn.First(&plugin, req.Msg.Id); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.GetPluginResponse{Plugin: toGrpcPlugin(plugin)}), nil
}

// UpdatePlugin implements evm_indexerv1connect.EvmIndexerServiceHandler. Changing
// the source resets the plugin to NOT_INSTALLED so it must be reinstalled.
func (e *EvmIndexerServer) UpdatePlugin(ctx context.Context, req *connect.Request[evm_indexerv1.UpdatePluginRequest]) (*connect.Response[evm_indexerv1.UpdatePluginResponse], error) {
	var plugin evmi_database.Plugin
	if result := e.db.Conn.First(&plugin, req.Msg.Plugin.Id); result.Error != nil {
		return nil, result.Error
	}

	sourceChanged := plugin.GithubUrl != req.Msg.Plugin.GithubUrl ||
		plugin.RelativePath != req.Msg.Plugin.RelativePath ||
		plugin.LocalPath != req.Msg.Plugin.LocalPath

	plugin.Name = req.Msg.Plugin.Name
	plugin.Description = req.Msg.Plugin.Description
	plugin.GithubUrl = req.Msg.Plugin.GithubUrl
	plugin.RelativePath = req.Msg.Plugin.RelativePath
	plugin.LocalPath = req.Msg.Plugin.LocalPath
	if sourceChanged {
		plugin.Status = string(evmi_database.NotInstalledPluginStatus)
		plugin.SoPath = ""
		plugin.Error = ""
	}

	if result := e.db.Conn.Save(&plugin); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.UpdatePluginResponse{}), nil
}

// ListPlugins implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListPlugins(ctx context.Context, req *connect.Request[evm_indexerv1.ListPluginsRequest]) (*connect.Response[evm_indexerv1.ListPluginsResponse], error) {
	var plugins []evmi_database.Plugin

	query := e.db.Conn.Model(&evmi_database.Plugin{})
	if req.Msg.Pagination != nil && req.Msg.Pagination.Limit > 0 {
		query = query.Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	}
	if result := query.Find(&plugins); result.Error != nil {
		return nil, result.Error
	}

	out := make([]*evm_indexerv1.Plugin, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, toGrpcPlugin(p))
	}
	return connect.NewResponse(&evm_indexerv1.ListPluginsResponse{Plugins: out}), nil
}

// DeletePlugin implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeletePlugin(ctx context.Context, req *connect.Request[evm_indexerv1.DeletePluginRequest]) (*connect.Response[evm_indexerv1.DeletePluginResponse], error) {
	// Refuse to delete a plugin still referenced by an exporter.
	var count int64
	if result := e.db.Conn.Model(&evmi_database.EvmiExporter{}).Where("plugin_id = ?", req.Msg.Id).Count(&count); result.Error != nil {
		return nil, result.Error
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errPluginInUse)
	}

	if result := e.db.Conn.Delete(&evmi_database.Plugin{}, req.Msg.Id); result.Error != nil {
		return nil, result.Error
	}
	return connect.NewResponse(&evm_indexerv1.DeletePluginResponse{}), nil
}

// InstallPlugin builds the plugin's shared object and records the result.
func (e *EvmIndexerServer) InstallPlugin(ctx context.Context, req *connect.Request[evm_indexerv1.InstallPluginRequest]) (*connect.Response[evm_indexerv1.InstallPluginResponse], error) {
	err := exporter.InstallPlugin(e.db, uint(req.Msg.Id), e.logger)

	var plugin evmi_database.Plugin
	e.db.Conn.First(&plugin, req.Msg.Id)

	if err != nil {
		return connect.NewResponse(&evm_indexerv1.InstallPluginResponse{
			Success: false,
			Error:   err.Error(),
			Status:  plugin.Status,
		}), nil
	}
	return connect.NewResponse(&evm_indexerv1.InstallPluginResponse{
		Success: true,
		Status:  plugin.Status,
	}), nil
}

func toGrpcPlugin(p evmi_database.Plugin) *evm_indexerv1.Plugin {
	id := uint32(p.ID)
	createdAt := uint32(p.CreatedAt.Unix())
	updatedAt := uint32(p.UpdatedAt.Unix())
	deletedAt := uint32(p.DeletedAt.Time.Unix())

	return &evm_indexerv1.Plugin{
		Id:           &id,
		Name:         p.Name,
		Description:  p.Description,
		GithubUrl:    p.GithubUrl,
		RelativePath: p.RelativePath,
		LocalPath:    p.LocalPath,
		SoPath:       p.SoPath,
		Status:       p.Status,
		Error:        p.Error,
		CreatedAt:    &createdAt,
		UpdatedAt:    &updatedAt,
		DeletedAt:    &deletedAt,
	}
}
