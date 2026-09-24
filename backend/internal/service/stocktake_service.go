package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"biosample-cold-custody-tracking/backend/internal/constants"
	"biosample-cold-custody-tracking/backend/internal/dto"
	"biosample-cold-custody-tracking/backend/internal/model"
	"biosample-cold-custody-tracking/backend/internal/repository"
	"biosample-cold-custody-tracking/backend/internal/util"
)

type StocktakeService interface {
	List(context.Context, repository.StocktakeFilter) (dto.PageResult[model.StocktakeTask], error)
	Get(context.Context, uint) (*model.StocktakeTask, error)
	ListItems(context.Context, uint, repository.StocktakeItemFilter) (dto.PageResult[model.StocktakeItem], error)
	Create(context.Context, Actor, dto.CreateStocktakeRequest) (*model.StocktakeTask, error)
	MarkItem(context.Context, Actor, uint, dto.MarkStocktakeItemRequest) (*model.StocktakeItem, error)
	Close(context.Context, Actor, uint) (*model.StocktakeTask, error)
	Cancel(context.Context, Actor, uint, dto.CancelStocktakeRequest) (*model.StocktakeTask, error)
}

type stocktakeService struct {
	repo  repository.StocktakeRepository
	audit AuditService
}

func NewStocktakeService(repo repository.StocktakeRepository, audit AuditService) StocktakeService {
	return &stocktakeService{repo: repo, audit: audit}
}

func (s *stocktakeService) List(ctx context.Context, filter repository.StocktakeFilter) (dto.PageResult[model.StocktakeTask], error) {
	query := filter.PageQuery.Normalize()
	filter.PageQuery = query
	items, total, err := s.repo.List(ctx, filter)
	return dto.PageResult[model.StocktakeTask]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (s *stocktakeService) Get(ctx context.Context, id uint) (*model.StocktakeTask, error) {
	return s.repo.Find(ctx, id)
}

func (s *stocktakeService) ListItems(ctx context.Context, taskID uint, filter repository.StocktakeItemFilter) (dto.PageResult[model.StocktakeItem], error) {
	if _, err := s.repo.Find(ctx, taskID); err != nil {
		return dto.PageResult[model.StocktakeItem]{}, err
	}
	query := filter.PageQuery.Normalize()
	filter.PageQuery = query
	if !filter.Pending {
		if result := strings.TrimSpace(filter.Result); result != "" && constants.StocktakeResult(result) != constants.StocktakeResultInStock &&
			constants.StocktakeResult(result) != constants.StocktakeResultMissing &&
			constants.StocktakeResult(result) != constants.StocktakeResultMismatched {
			return dto.PageResult[model.StocktakeItem]{}, util.BadRequest("不支持的盘点结果筛选")
		}
	}
	items, total, err := s.repo.ListItems(ctx, taskID, filter)
	return dto.PageResult[model.StocktakeItem]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (s *stocktakeService) Create(ctx context.Context, actor Actor, input dto.CreateStocktakeRequest) (*model.StocktakeTask, error) {
	number := strings.ToUpper(strings.TrimSpace(input.TaskNo))
	if _, err := s.repo.FindByNumber(ctx, number); err == nil {
		return nil, util.Conflict("盘点单号已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	task := &model.StocktakeTask{
		TaskNo:        number,
		ContainerID:   input.ContainerID,
		State:         constants.StocktakeStateInProgress,
		StartedByID:   actor.ID,
		StartedByName: actor.Name,
		StartedAt:     time.Now().UTC(),
		Note:          strings.TrimSpace(input.Note),
	}
	task.Normalize()
	if err := task.Validate(); err != nil {
		return nil, util.BadRequest(err.Error())
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.started", "StocktakeTask", task.ID, nil, task); err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, task.ID)
}

func (s *stocktakeService) MarkItem(ctx context.Context, actor Actor, itemID uint, input dto.MarkStocktakeItemRequest) (*model.StocktakeItem, error) {
	remark := strings.TrimSpace(input.Remark)
	position := ""
	if input.NewPosition != nil {
		position = strings.TrimSpace(*input.NewPosition)
	}
	switch input.Result {
	case constants.StocktakeResultMissing:
		if len([]rune(remark)) < 3 {
			return nil, util.BadRequest("缺失样本必须填写至少 3 个字符的情况说明")
		}
	case constants.StocktakeResultMismatched:
		if input.NewContainerID == nil || *input.NewContainerID == 0 || position == "" {
			return nil, util.BadRequest("位置不符必须选择新容器和新格位")
		}
	case constants.StocktakeResultInStock:
		// 在库无需说明与新位置。
	}
	mark := repository.StocktakeItemMark{
		Result:         input.Result,
		Remark:         remark,
		NewContainerID: input.NewContainerID,
		NewPosition:    position,
		CheckedByID:    actor.ID,
		CheckedByName:  actor.Name,
		CheckedAt:      time.Now().UTC(),
	}
	item, _, before, err := s.repo.MarkItem(ctx, itemID, mark)
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.item_marked", "StocktakeItem", itemID, before, item); err != nil {
		return nil, err
	}
	return s.repo.FindItem(ctx, itemID)
}

func (s *stocktakeService) Close(ctx context.Context, actor Actor, taskID uint) (*model.StocktakeTask, error) {
	beforeTask, err := s.repo.Find(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if beforeTask.State != constants.StocktakeStateInProgress {
		return nil, util.Conflict("盘点任务已结束")
	}
	if !beforeTask.AllResolved() {
		return nil, util.Conflict(fmt.Sprintf("还有 %d 支样本未标记，全部条目处理完才能关单", beforeTask.PendingCount))
	}
	task, beforeSpecimens, afterSpecimens, containers, err := s.repo.Close(ctx, taskID, actor.ID, actor.Name, time.Now().UTC())
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.closed", "StocktakeTask", taskID, beforeTask, task); err != nil {
		return nil, err
	}
	beforeByID := map[uint]model.Specimen{}
	for _, specimen := range beforeSpecimens {
		beforeByID[specimen.ID] = specimen
	}
	for _, specimen := range afterSpecimens {
		before := beforeByID[specimen.ID]
		if err := s.audit.Record(ctx, actor, "specimen.relocated", "Specimen", specimen.ID, before, specimen); err != nil {
			return nil, err
		}
	}
	for _, container := range containers {
		if err := s.audit.Record(ctx, actor, "storage_container.occupied_adjusted", "StorageContainer", container.ID, nil, container); err != nil {
			return nil, err
		}
	}
	return task, nil
}

func (s *stocktakeService) Cancel(ctx context.Context, actor Actor, taskID uint, input dto.CancelStocktakeRequest) (*model.StocktakeTask, error) {
	beforeTask, err := s.repo.Find(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if beforeTask.State != constants.StocktakeStateInProgress {
		return nil, util.Conflict("盘点任务已结束，不能取消")
	}
	task, err := s.repo.Cancel(ctx, taskID, actor.ID, actor.Name, input.Reason, time.Now().UTC())
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.cancelled", "StocktakeTask", taskID, beforeTask, task); err != nil {
		return nil, err
	}
	return task, nil
}

func mapStocktakeError(err error) error {
	switch {
	case errors.Is(err, repository.ErrStocktakeFinished):
		return util.Conflict("盘点任务已结束")
	case errors.Is(err, repository.ErrStocktakeContainerBusy):
		return util.Conflict("该容器已有进行中的盘点任务，完成或取消后才能重新盘点")
	case errors.Is(err, repository.ErrStocktakeContainerUnusable):
		return util.BadRequest("只能选择启用且可用状态的冻存容器")
	case errors.Is(err, repository.ErrStocktakeContainerEmpty):
		return util.BadRequest("该容器当前没有在库样本，无需盘点")
	case errors.Is(err, repository.ErrStocktakeHasPending):
		return util.Conflict("还有样本未标记，全部条目处理完才能关单")
	case errors.Is(err, repository.ErrStocktakeItemDrifted):
		return util.Conflict("盘点开始后样本已交接或位置已变化，旧清单不能覆盖现位置，本次任务只能取消后重新盘点")
	case errors.Is(err, repository.ErrStocktakeSlotOccupied):
		return util.Conflict("新格位已被占用，请重新选择")
	case errors.Is(err, repository.ErrStocktakeTargetFull):
		return util.Conflict("目标容器不可用或容量不足")
	case errors.Is(err, repository.ErrStocktakeNewPositionSame):
		return util.BadRequest("位置不符时新容器和新格位不能与原位置完全相同")
	case errors.Is(err, repository.ErrStocktakeTaskNoTaken):
		return util.Conflict("盘点单号已存在")
	default:
		return err
	}
}
