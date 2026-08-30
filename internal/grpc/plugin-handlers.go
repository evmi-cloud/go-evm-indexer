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
	subPath, err := exporter.ValidatePluginPath(req.Msg.Plugin.Path)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	plugin := evmi_database.Plugin{
		Name:        req.Msg.Plugin.Name,
		Description: req.Msg.Plugin.Description,
		GitUrl:      req.Msg.Plugin.GitUrl,
		GitRef:      req.Msg.Plugin.GitRef,
		Path:        subPath,
		Status:      string(evmi_database.NotInstalledPluginStatus),
	}
	if result := e.db.Conn.Create(&plugin); result.Error != nil {
		return nil, dbError(result.Error)
	}
	return connect.NewResponse(&evm_indexerv1.CreatePluginResponse{Id: uint32(plugin.ID)}), nil
}

// GetPlugin implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetPlugin(ctx context.Context, req *connect.Request[evm_indexerv1.GetPluginRequest]) (*connect.Response[evm_indexerv1.GetPluginResponse], error) {
	var plugin evmi_database.Plugin
	if result := e.db.Conn.First(&plugin, req.Msg.Id); result.Error != nil {
		return nil, dbError(result.Error)
	}
	return connect.NewResponse(&evm_indexerv1.GetPluginResponse{Plugin: toGrpcPlugin(plugin)}), nil
}

// UpdatePlugin implements evm_indexerv1connect.EvmIndexerServiceHandler. Changing
// the source resets the plugin to NOT_INSTALLED so it must be reinstalled.
func (e *EvmIndexerServer) UpdatePlugin(ctx context.Context, req *connect.Request[evm_indexerv1.UpdatePluginRequest]) (*connect.Response[evm_indexerv1.UpdatePluginResponse], error) {
	subPath, err := exporter.ValidatePluginPath(req.Msg.Plugin.Path)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	var plugin evmi_database.Plugin
	if result := e.db.Conn.First(&plugin, req.Msg.Plugin.Id); result.Error != nil {
		return nil, dbError(result.Error)
	}

	// The in-repo path is part of the source: pointing the plugin at another
	// directory of the same repo builds different code.
	sourceChanged := plugin.GitUrl != req.Msg.Plugin.GitUrl ||
		plugin.GitRef != req.Msg.Plugin.GitRef ||
		plugin.Path != subPath

	plugin.Name = req.Msg.Plugin.Name
	plugin.Description = req.Msg.Plugin.Description
	plugin.GitUrl = req.Msg.Plugin.GitUrl
	plugin.GitRef = req.Msg.Plugin.GitRef
	plugin.Path = subPath
	if sourceChanged {
		plugin.Status = string(evmi_database.NotInstalledPluginStatus)
		plugin.BinaryPath = ""
		plugin.Error = ""
		plugin.ConfigSchema = nil
	}

	if result := e.db.Conn.Save(&plugin); result.Error != nil {
		return nil, dbError(result.Error)
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
		return nil, dbError(result.Error)
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
		return nil, dbError(result.Error)
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errPluginInUse)
	}

	if result := e.db.Conn.Delete(&evmi_database.Plugin{}, req.Msg.Id); result.Error != nil {
		return nil, dbError(result.Error)
	}
	return connect.NewResponse(&evm_indexerv1.DeletePluginResponse{}), nil
}

// InstallPlugin builds the plugin executable and records the result.
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

// ListPluginGitRefs lists a git repo's branches and tags (via `git ls-remote`)
// so the UI can offer them as a select for the plugin's git ref.
func (e *EvmIndexerServer) ListPluginGitRefs(ctx context.Context, req *connect.Request[evm_indexerv1.ListPluginGitRefsRequest]) (*connect.Response[evm_indexerv1.ListPluginGitRefsResponse], error) {
	branches, tags, err := exporter.ListGitRefs(req.Msg.GitUrl)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&evm_indexerv1.ListPluginGitRefsResponse{
		Branches: branches,
		Tags:     tags,
	}), nil
}

// ListPluginCatalog lists the plugins a repository declares in its catalog file
// (the server shallow-clones it into a temp dir and reads the catalog), so the UI
// can offer them when a single repo hosts several plugins. A repo without a
// catalog returns no entries rather than an error — the path is then typed by
// hand.
func (e *EvmIndexerServer) ListPluginCatalog(ctx context.Context, req *connect.Request[evm_indexerv1.ListPluginCatalogRequest]) (*connect.Response[evm_indexerv1.ListPluginCatalogResponse], error) {
	entries, catalogPath, err := exporter.FetchPluginCatalog(req.Msg.GitUrl, req.Msg.GitRef, e.logger)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	out := make([]*evm_indexerv1.PluginCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, &evm_indexerv1.PluginCatalogEntry{
			Name:        entry.Name,
			Description: entry.Description,
			Path:        entry.Path,
		})
	}
	return connect.NewResponse(&evm_indexerv1.ListPluginCatalogResponse{
		Entries:     out,
		CatalogPath: catalogPath,
	}), nil
}

func toGrpcPlugin(p evmi_database.Plugin) *evm_indexerv1.Plugin {
	id := uint32(p.ID)
	createdAt := uint32(p.CreatedAt.Unix())
	updatedAt := uint32(p.UpdatedAt.Unix())
	deletedAt := uint32(p.DeletedAt.Time.Unix())

	return &evm_indexerv1.Plugin{
		Id:               &id,
		Name:             p.Name,
		Description:      p.Description,
		GitUrl:           p.GitUrl,
		GitRef:           p.GitRef,
		Path:             p.Path,
		BinaryPath:       p.BinaryPath,
		Status:           p.Status,
		Error:            p.Error,
		ConfigSchemaJson: string(p.ConfigSchema),
		CreatedAt:        &createdAt,
		UpdatedAt:        &updatedAt,
		DeletedAt:        &deletedAt,
	}
}
