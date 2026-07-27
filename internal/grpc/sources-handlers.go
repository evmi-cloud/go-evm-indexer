package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	internal_bus "github.com/evmi-cloud/go-evm-indexer/internal/bus"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	log_stores "github.com/evmi-cloud/go-evm-indexer/internal/database/log-stores"
	evm_indexerv1 "github.com/evmi-cloud/go-evm-indexer/internal/grpc/generated/evm_indexer/v1"
)

// CreateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) CreateEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.CreateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.CreateEvmLogSourceResponse], error) {
	newLogSource := evmi_database.EvmLogSource{
		Type:       req.Msg.Source.Type,
		StartBlock: req.Msg.Source.StartBlock,
		SyncBlock:  req.Msg.Source.SyncBlock,
		Address: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.Address),
			Valid:  IsNotNil(req.Msg.Source.Address),
		},
		Topic0: sql.NullString{
			String: DerefOrEmpty(req.Msg.Source.Topic0),
			Valid:  IsNotNil(req.Msg.Source.Topic0),
		},
		TopicFilters: req.Msg.Source.TopicFilters,

		EvmLogPipelineID: uint(req.Msg.Source.EvmLogPipelineId),
		EvmJsonAbiID:     uint(req.Msg.Source.EvmJsonAbiId),
		EvmBlockchainID:  uint(req.Msg.Source.EvmBlockchainId),
	}

	result := e.db.Conn.Create(&newLogSource)
	if result.Error != nil {
		return nil, dbError(result.Error)
	}

	// FACTORY sources carry N creation rules (recursive tree).
	if err := e.saveFactoryRules(newLogSource.ID, req.Msg.Source.FactoryRules); err != nil {
		return nil, dbError(err)
	}

	return &connect.Response[evm_indexerv1.CreateEvmLogSourceResponse]{
		Msg: &evm_indexerv1.CreateEvmLogSourceResponse{
			Id: uint32(newLogSource.ID),
		},
	}, nil
}

// DeleteEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) DeleteEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.DeleteEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.DeleteEvmLogSourceResponse], error) {
	var logSource evmi_database.EvmLogSource
	if err := e.db.Conn.First(&logSource, req.Msg.Id).Error; err != nil {
		return nil, dbError(err)
	}
	// Factory-created child sources are managed by their parent factory and can't be
	// deleted directly — only their indexation can be started/stopped. (Deleting the
	// parent factory cascades to them below.)
	if logSource.ParentSourceID != 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this source was created by a factory and cannot be deleted; only its indexation can be started or stopped"))
	}

	// Deleting a source deletes its whole subtree: the source itself plus every
	// factory-spawned descendant (recursively). A FACTORY source can have children,
	// which can themselves be factories with children.
	ids, err := e.collectSourceSubtree(logSource.ID)
	if err != nil {
		return nil, dbError(err)
	}

	// Stop indexing for every source about to be deleted so no worker keeps polling
	// or writing after its rows/data are gone.
	for _, id := range ids {
		e.bus.Emit(context.Background(), internal_bus.DisableSourceTopic, id)
	}

	// Delete the stored logs & transactions for every source in the subtree. Factory
	// children inherit their parent's pipeline (hence store), so one store handles
	// the whole subtree.
	if err := e.deleteSubtreeStoreData(logSource, ids); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Delete each source's factory rules (+conditions), then the source rows.
	for _, id := range ids {
		if err := e.deleteFactoryRules(id); err != nil {
			return nil, dbError(err)
		}
	}
	if err := e.db.Conn.Where("id IN ?", ids).Delete(&evmi_database.EvmLogSource{}).Error; err != nil {
		return nil, dbError(err)
	}

	return &connect.Response[evm_indexerv1.DeleteEvmLogSourceResponse]{
		Msg: &evm_indexerv1.DeleteEvmLogSourceResponse{},
	}, nil
}

// collectSourceSubtree returns rootID followed by every descendant source id
// (breadth-first over parent_source_id), so deleting walks the whole factory tree.
func (e *EvmIndexerServer) collectSourceSubtree(rootID uint) ([]uint, error) {
	ids := []uint{rootID}
	for queue := []uint{rootID}; len(queue) > 0; {
		parent := queue[0]
		queue = queue[1:]
		var children []evmi_database.EvmLogSource
		if err := e.db.Conn.Where("parent_source_id = ?", parent).Find(&children).Error; err != nil {
			return nil, err
		}
		for _, c := range children {
			ids = append(ids, c.ID)
			queue = append(queue, c.ID)
		}
	}
	return ids, nil
}

// deleteSubtreeStoreData removes the stored logs and transactions of every source
// id from the pipeline's log store. The store is resolved from the (top) source's
// pipeline, which all factory children share.
func (e *EvmIndexerServer) deleteSubtreeStoreData(source evmi_database.EvmLogSource, ids []uint) error {
	var pipeline evmi_database.EvmLogPipeline
	if err := e.db.Conn.First(&pipeline, source.EvmLogPipelineID).Error; err != nil {
		return err
	}
	var storeInfo evmi_database.EvmLogStore
	if err := e.db.Conn.First(&storeInfo, pipeline.EvmLogStoreId).Error; err != nil {
		return err
	}
	var storeConfig map[string]string
	if err := json.Unmarshal(storeInfo.StoreConfig, &storeConfig); err != nil {
		return err
	}
	store, err := log_stores.LoadStore(storeInfo.StoreType, storeConfig, e.logger)
	if err != nil {
		return err
	}
	storage := store.GetStorage()
	for _, id := range ids {
		if err := storage.DeleteSourceData(uint64(id)); err != nil {
			return err
		}
	}
	return nil
}

// GetEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) GetEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.GetEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.GetEvmLogSourceResponse], error) {
	var logSource evmi_database.EvmLogSource

	result := e.db.Conn.First(&logSource, req.Msg.Id)
	if result.Error != nil {
		return nil, dbError(result.Error)
	}

	return &connect.Response[evm_indexerv1.GetEvmLogSourceResponse]{
		Msg: &evm_indexerv1.GetEvmLogSourceResponse{
			Source: e.toGrpcLogSource(logSource),
		},
	}, nil
}

// ListEvmLogSources implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) ListEvmLogSources(ctx context.Context, req *connect.Request[evm_indexerv1.ListEvmLogSourcesRequest]) (*connect.Response[evm_indexerv1.ListEvmLogSourcesResponse], error) {
	var logSources []evmi_database.EvmLogSource

	query := e.db.Conn.Model(&evmi_database.EvmLogSource{})
	if req.Msg.Pagination != nil && req.Msg.Pagination.Limit > 0 {
		query = query.Offset(int(req.Msg.Pagination.Offset)).Limit(int(req.Msg.Pagination.Limit))
	}
	result := query.Find(&logSources)
	if result.Error != nil {
		return nil, dbError(result.Error)
	}

	return &connect.Response[evm_indexerv1.ListEvmLogSourcesResponse]{
		Msg: &evm_indexerv1.ListEvmLogSourcesResponse{
			Sources: e.toGrpcLogSources(logSources),
		},
	}, nil
}

// UpdateEvmLogSource implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) UpdateEvmLogSource(ctx context.Context, req *connect.Request[evm_indexerv1.UpdateEvmLogSourceRequest]) (*connect.Response[evm_indexerv1.UpdateEvmLogSourceResponse], error) {
	var logSoure evmi_database.EvmLogSource

	result := e.db.Conn.First(&logSoure, req.Msg.Source.Id)
	if result.Error != nil {
		return nil, dbError(result.Error)
	}

	// Factory-created child sources are managed by their parent factory and can't be
	// edited — only their indexation can be started/stopped.
	if logSoure.ParentSourceID != 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this source was created by a factory and cannot be edited; only its indexation can be started or stopped"))
	}

	logSoure.Type = req.Msg.Source.Type
	logSoure.StartBlock = req.Msg.Source.StartBlock
	logSoure.SyncBlock = req.Msg.Source.SyncBlock
	logSoure.Address = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.Address),
		Valid:  IsNotNil(req.Msg.Source.Address),
	}
	logSoure.Topic0 = sql.NullString{
		String: DerefOrEmpty(req.Msg.Source.Topic0),
		Valid:  IsNotNil(req.Msg.Source.Topic0),
	}
	logSoure.TopicFilters = req.Msg.Source.TopicFilters

	logSoure.EvmLogPipelineID = uint(req.Msg.Source.EvmLogPipelineId)
	logSoure.EvmJsonAbiID = uint(req.Msg.Source.EvmJsonAbiId)
	logSoure.EvmBlockchainID = uint(req.Msg.Source.EvmBlockchainId)

	result = e.db.Conn.Save(&logSoure)
	if result.Error != nil {
		return nil, dbError(result.Error)
	}

	// Replace the source's factory rule tree (spawned children keep their own
	// cloned rules, which live under a different source id).
	if err := e.saveFactoryRules(logSoure.ID, req.Msg.Source.FactoryRules); err != nil {
		return nil, dbError(err)
	}

	return &connect.Response[evm_indexerv1.UpdateEvmLogSourceResponse]{
		Msg: &evm_indexerv1.UpdateEvmLogSourceResponse{},
	}, nil
}

// StartSourceIndexer implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StartSourceIndexer(ctx context.Context, req *connect.Request[evm_indexerv1.StartSourceIndexerRequest]) (*connect.Response[evm_indexerv1.StartSourceIndexerResponse], error) {

	sourceId := uint(req.Msg.Id)
	var source evmi_database.EvmLogSource
	result := e.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return &connect.Response[evm_indexerv1.StartSourceIndexerResponse]{
			Msg: &evm_indexerv1.StartSourceIndexerResponse{
				Success: false,
				Error:   result.Error.Error(),
			},
		}, nil
	}

	e.bus.Emit(context.Background(), internal_bus.EnableSourceTopic, sourceId)

	try := 10
	for i := 0; i < try; i++ {
		time.Sleep(time.Second)
		result := e.db.Conn.First(&source, sourceId)
		if result.Error != nil {
			return &connect.Response[evm_indexerv1.StartSourceIndexerResponse]{
				Msg: &evm_indexerv1.StartSourceIndexerResponse{
					Success: false,
					Error:   result.Error.Error(),
				},
			}, nil
		}

		if source.Status == string(evmi_database.RunningLogSourceStatus) {
			return &connect.Response[evm_indexerv1.StartSourceIndexerResponse]{
				Msg: &evm_indexerv1.StartSourceIndexerResponse{
					Success: true,
				},
			}, nil
		}
	}

	return &connect.Response[evm_indexerv1.StartSourceIndexerResponse]{
		Msg: &evm_indexerv1.StartSourceIndexerResponse{},
	}, nil
}

// StopSourceIndexer implements evm_indexerv1connect.EvmIndexerServiceHandler.
func (e *EvmIndexerServer) StopSourceIndexer(ctx context.Context, req *connect.Request[evm_indexerv1.StopSourceIndexerRequest]) (*connect.Response[evm_indexerv1.StopSourceIndexerResponse], error) {
	sourceId := uint(req.Msg.Id)
	var source evmi_database.EvmLogSource
	result := e.db.Conn.First(&source, sourceId)
	if result.Error != nil {
		return &connect.Response[evm_indexerv1.StopSourceIndexerResponse]{
			Msg: &evm_indexerv1.StopSourceIndexerResponse{
				Success: false,
				Error:   result.Error.Error(),
			},
		}, nil
	}

	e.bus.Emit(context.Background(), internal_bus.DisableSourceTopic, sourceId)

	try := 10
	for i := 0; i < try; i++ {
		time.Sleep(time.Second)
		result := e.db.Conn.First(&source, sourceId)
		if result.Error != nil {
			return &connect.Response[evm_indexerv1.StopSourceIndexerResponse]{
				Msg: &evm_indexerv1.StopSourceIndexerResponse{
					Success: false,
					Error:   result.Error.Error(),
				},
			}, nil
		}

		if source.Status == string(evmi_database.StoppedLogSourceStatus) {
			return &connect.Response[evm_indexerv1.StopSourceIndexerResponse]{
				Msg: &evm_indexerv1.StopSourceIndexerResponse{
					Success: true,
				},
			}, nil
		}
	}

	return &connect.Response[evm_indexerv1.StopSourceIndexerResponse]{
		Msg: &evm_indexerv1.StopSourceIndexerResponse{},
	}, nil
}

func DerefOrEmpty[T any](val *T) T {
	if val == nil {
		var empty T
		return empty
	}
	return *val
}

func IsNotNil[T any](val *T) bool {
	return val != nil
}

func (e *EvmIndexerServer) toGrpcLogSource(logSource evmi_database.EvmLogSource) *evm_indexerv1.EvmLogSource {
	id := uint32(logSource.ID)
	createdAt := uint32(logSource.CreatedAt.Unix())
	updatedAt := uint32(logSource.UpdatedAt.Unix())
	deletedAt := uint32(logSource.DeletedAt.Time.Unix())
	// Only FACTORY sources have creation rules; skip the DB read otherwise.
	var rules []*evm_indexerv1.FactoryRule
	if logSource.Type == string(evmi_database.FactoryLogSourceType) {
		rules, _ = e.loadFactoryRules(logSource.ID)
	}
	return &evm_indexerv1.EvmLogSource{
		Id:               &id,
		Type:             string(logSource.Type),
		Enabled:          logSource.Enabled,
		Status:           string(logSource.Status),
		StartBlock:       logSource.StartBlock,
		SyncBlock:        logSource.SyncBlock,
		Address:          &logSource.Address.String,
		Topic0:           &logSource.Topic0.String,
		TopicFilters:     logSource.TopicFilters,
		FactoryRules:     rules,
		ParentSourceId:   uint32(logSource.ParentSourceID),
		EvmBlockchainId:  uint32(logSource.EvmBlockchainID),
		EvmLogPipelineId: uint32(logSource.EvmLogPipelineID),
		EvmJsonAbiId:     uint32(logSource.EvmJsonAbiID),

		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
		DeletedAt: &deletedAt,
	}
}

func (e *EvmIndexerServer) toGrpcLogSources(logSources []evmi_database.EvmLogSource) []*evm_indexerv1.EvmLogSource {
	result := make([]*evm_indexerv1.EvmLogSource, 0, len(logSources))
	for _, logSource := range logSources {
		result = append(result, e.toGrpcLogSource(logSource))
	}
	return result
}

// --- factory rule tree persistence (recursive) ---

// saveFactoryRules replaces a source's factory rule tree with the given rules
// (rules cloned onto spawned children live under a different source id and are
// untouched).
func (e *EvmIndexerServer) saveFactoryRules(sourceID uint, rules []*evm_indexerv1.FactoryRule) error {
	if err := e.deleteFactoryRules(sourceID); err != nil {
		return err
	}
	return e.createFactoryRules(sourceID, nil, rules)
}

// createFactoryRules persists rules under an owner: a nil parentRuleID means these
// are the source's top-level rules (EvmLogSourceID set, ParentRuleID NULL); a
// non-nil parentRuleID means they are nested under that rule (ParentRuleID set,
// EvmLogSourceID NULL).
func (e *EvmIndexerServer) createFactoryRules(sourceID uint, parentRuleID *uint, rules []*evm_indexerv1.FactoryRule) error {
	for _, r := range rules {
		row := evmi_database.EvmFactoryRule{
			ParentRuleID:          parentRuleID,
			CreationFunctionName:  r.CreationFunctionName,
			CreationAddressLogArg: r.CreationAddressLogArg,
			ChildType:             r.ChildType,
			EvmJsonAbiID:          uint(r.EvmJsonAbiId),
		}
		if parentRuleID == nil {
			sid := sourceID
			row.EvmLogSourceID = &sid
		}
		for _, c := range r.Conditions {
			row.Conditions = append(row.Conditions, evmi_database.EvmFactoryRuleCondition{
				Arg: c.Arg, Operator: c.Operator, Value: c.Value,
			})
		}
		if err := e.db.Conn.Create(&row).Error; err != nil {
			return err
		}
		if len(r.ChildRules) > 0 {
			rid := row.ID
			if err := e.createFactoryRules(sourceID, &rid, r.ChildRules); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *EvmIndexerServer) deleteFactoryRules(sourceID uint) error {
	var top []evmi_database.EvmFactoryRule
	if err := e.db.Conn.Where("evm_log_source_id = ? AND parent_rule_id IS NULL", sourceID).Find(&top).Error; err != nil {
		return err
	}
	for _, r := range top {
		if err := e.deleteRuleSubtree(r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *EvmIndexerServer) deleteRuleSubtree(ruleID uint) error {
	var children []evmi_database.EvmFactoryRule
	if err := e.db.Conn.Where("parent_rule_id = ?", ruleID).Find(&children).Error; err != nil {
		return err
	}
	for _, c := range children {
		if err := e.deleteRuleSubtree(c.ID); err != nil {
			return err
		}
	}
	if err := e.db.Conn.Unscoped().Where("evm_factory_rule_id = ?", ruleID).Delete(&evmi_database.EvmFactoryRuleCondition{}).Error; err != nil {
		return err
	}
	return e.db.Conn.Unscoped().Delete(&evmi_database.EvmFactoryRule{}, ruleID).Error
}

func (e *EvmIndexerServer) loadFactoryRules(sourceID uint) ([]*evm_indexerv1.FactoryRule, error) {
	return e.loadRulesAt(sourceID, nil)
}

// loadRulesAt returns the rules under an owner: a nil parentRuleID loads the
// source's top-level rules (parent_rule_id IS NULL), a non-nil one loads that
// rule's nested children.
func (e *EvmIndexerServer) loadRulesAt(sourceID uint, parentRuleID *uint) ([]*evm_indexerv1.FactoryRule, error) {
	var rows []evmi_database.EvmFactoryRule
	q := e.db.Conn.Preload("Conditions")
	if parentRuleID != nil {
		q = q.Where("parent_rule_id = ?", *parentRuleID)
	} else {
		q = q.Where("evm_log_source_id = ? AND parent_rule_id IS NULL", sourceID)
	}
	if err := q.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*evm_indexerv1.FactoryRule, 0, len(rows))
	for _, row := range rows {
		rid := uint32(row.ID)
		ruleID := row.ID
		children, err := e.loadRulesAt(0, &ruleID)
		if err != nil {
			return nil, err
		}
		conditions := make([]*evm_indexerv1.FactoryRuleCondition, 0, len(row.Conditions))
		for _, c := range row.Conditions {
			conditions = append(conditions, &evm_indexerv1.FactoryRuleCondition{
				Arg: c.Arg, Operator: c.Operator, Value: c.Value,
			})
		}
		out = append(out, &evm_indexerv1.FactoryRule{
			Id:                    &rid,
			CreationFunctionName:  row.CreationFunctionName,
			CreationAddressLogArg: row.CreationAddressLogArg,
			ChildType:             row.ChildType,
			EvmJsonAbiId:          uint32(row.EvmJsonAbiID),
			ChildRules:            children,
			Conditions:            conditions,
		})
	}
	return out, nil
}
