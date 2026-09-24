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
	List(context.Context, repository.StocktakeFilter) (dto.PageResult[model.Stocktake], error)
	Get(context.Context, uint) (*model.Stocktake, error)
	Create(context.Context, Actor, dto.CreateStocktakeRequest) (*model.Stocktake, error)
	MarkItem(context.Context, Actor, uint, uint, dto.MarkStocktakeItemRequest) (*model.Stocktake, error)
	Close(context.Context, Actor, uint) (*model.Stocktake, error)
	Cancel(context.Context, Actor, uint, string) (*model.Stocktake, error)
}

type stocktakeService struct {
	repo        repository.StocktakeRepository
	storageRepo repository.StorageRepository
	audit       AuditService
}

func NewStocktakeService(repo repository.StocktakeRepository, storageRepo repository.StorageRepository, audit AuditService) StocktakeService {
	return &stocktakeService{repo: repo, storageRepo: storageRepo, audit: audit}
}

func (s *stocktakeService) List(ctx context.Context, filter repository.StocktakeFilter) (dto.PageResult[model.Stocktake], error) {
	query := filter.PageQuery.Normalize()
	filter.PageQuery = query
	items, total, err := s.repo.List(ctx, filter)
	return dto.PageResult[model.Stocktake]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, err
}

func (s *stocktakeService) Get(ctx context.Context, id uint) (*model.Stocktake, error) {
	return s.repo.Find(ctx, id)
}

func (s *stocktakeService) Create(ctx context.Context, actor Actor, input dto.CreateStocktakeRequest) (*model.Stocktake, error) {
	container, err := s.storageRepo.Find(ctx, input.ContainerID)
	if err != nil {
		return nil, err
	}
	if !container.Active || container.Status != "available" {
		return nil, util.Conflict("只能对启用且可用的冻存容器发起盘点")
	}
	stored, err := s.storageRepo.CountStoredSpecimens(ctx, container.ID)
	if err != nil {
		return nil, err
	}
	if stored == 0 {
		return nil, util.Conflict("容器内没有在库样本，无法生成盘点清单")
	}
	now := time.Now().UTC()
	number, err := s.nextNumber(ctx, now)
	if err != nil {
		return nil, err
	}
	task := &model.Stocktake{
		StocktakeNo:   number,
		TaskMonth:     now.Format("2006-01"),
		ContainerID:   container.ID,
		State:         constants.StocktakeStateInProgress,
		StartedByID:   actor.ID,
		StartedByName: actor.Name,
		StartedAt:     now,
	}
	if err := s.repo.CreateWithSnapshot(ctx, task); err != nil {
		return nil, mapStocktakeError(err)
	}
	created, err := s.repo.Find(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if err := s.audit.Record(ctx, actor, "stocktake.prepared", "Stocktake", created.ID, nil, created); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *stocktakeService) MarkItem(ctx context.Context, actor Actor, stocktakeID, itemID uint, input dto.MarkStocktakeItemRequest) (*model.Stocktake, error) {
	task, err := s.repo.Find(ctx, stocktakeID)
	if err != nil {
		return nil, err
	}
	if task.State != constants.StocktakeStateInProgress {
		return nil, util.Conflict("盘点单已结束，不能再标记条目")
	}
	note := strings.TrimSpace(input.Note)
	if input.Result == constants.StocktakeResultMissing && len([]rune(note)) < 2 {
		return nil, util.BadRequest("标记缺失必须填写缺失说明")
	}
	newPosition := strings.TrimSpace(input.NewPosition)
	newContainerID := input.NewContainerID
	if input.Result == constants.StocktakeResultMislocated {
		if newContainerID == nil || *newContainerID == 0 {
			return nil, util.BadRequest("位置不符必须选择新容器")
		}
		if newPosition == "" {
			return nil, util.BadRequest("位置不符必须填写新格位")
		}
		target, err := s.storageRepo.Find(ctx, *newContainerID)
		if err != nil {
			return nil, err
		}
		if !target.Active || target.Status != "available" {
			return nil, util.Conflict("新容器未启用或当前不可用")
		}
	} else {
		newContainerID = nil
		newPosition = ""
	}
	mark := repository.StocktakeMark{
		Result:           input.Result,
		Note:             note,
		NewContainerID:   newContainerID,
		NewPosition:      newPosition,
		NewContainerCode: "",
		MarkedByID:       actor.ID,
		MarkedByName:     actor.Name,
		MarkedAt:         time.Now().UTC(),
	}
	updated, err := s.repo.MarkItem(ctx, stocktakeID, itemID, mark)
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.item_marked", "Stocktake", stocktakeID, task, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *stocktakeService) Close(ctx context.Context, actor Actor, stocktakeID uint) (*model.Stocktake, error) {
	task, err := s.repo.Find(ctx, stocktakeID)
	if err != nil {
		return nil, err
	}
	if task.State != constants.StocktakeStateInProgress {
		return nil, util.Conflict("盘点单已结束，不能重复关单")
	}
	if !task.AllProcessed() {
		return nil, util.Conflict(fmt.Sprintf("仍有 %d 条清单未处理，全部条目标记完成后才能关单", task.TotalItems-task.ProcessedItems))
	}
	resolution := repository.StocktakeResolution{
		ClosedByID:   actor.ID,
		ClosedByName: actor.Name,
		ClosedAt:     time.Now().UTC(),
	}
	outcome, err := s.repo.Close(ctx, stocktakeID, resolution)
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.closed", "Stocktake", stocktakeID, task, outcome.Stocktake); err != nil {
		return nil, err
	}
	for _, change := range outcome.Changes {
		action := "specimen.stocktake_mislocated"
		if change.After.State == constants.SpecimenStateDisposed {
			action = "specimen.stocktake_missing"
		}
		if err := s.audit.Record(ctx, actor, action, "Specimen", change.After.ID, change.Before, change.After); err != nil {
			return nil, err
		}
	}
	return outcome.Stocktake, nil
}

func (s *stocktakeService) Cancel(ctx context.Context, actor Actor, stocktakeID uint, reason string) (*model.Stocktake, error) {
	task, err := s.repo.Find(ctx, stocktakeID)
	if err != nil {
		return nil, err
	}
	if task.State != constants.StocktakeStateInProgress {
		return nil, util.Conflict("盘点单已结束，不能取消")
	}
	reason = strings.TrimSpace(reason)
	resolution := repository.StocktakeResolution{
		ClosedByID:   actor.ID,
		ClosedByName: actor.Name,
		ClosedAt:     time.Now().UTC(),
		CancelReason: reason,
	}
	cancelled, err := s.repo.Cancel(ctx, stocktakeID, resolution)
	if err != nil {
		return nil, mapStocktakeError(err)
	}
	if err := s.audit.Record(ctx, actor, "stocktake.cancelled", "Stocktake", stocktakeID, task, cancelled); err != nil {
		return nil, err
	}
	return cancelled, nil
}

func (s *stocktakeService) nextNumber(ctx context.Context, now time.Time) (string, error) {
	taskMonth := now.Format("2006-01")
	compact := now.Format("20060102")
	count, err := s.repo.CountByMonth(ctx, taskMonth)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 5; attempt++ {
		number := fmt.Sprintf("ST-%s-%03d", compact, count+int64(attempt)+1)
		if _, err := s.repo.FindByNumber(ctx, number); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return number, nil
			}
			return "", err
		}
	}
	return "", util.Conflict("盘点单号生成冲突，请稍后重试")
}

func mapStocktakeError(err error) error {
	var drift *repository.StocktakeDriftError
	switch {
	case errors.As(err, &drift):
		return util.Conflict("盘点开始后这些样本已交接或位置已变化，旧清单不能覆盖现位置，本次任务只能取消后重新盘点：" + strings.Join(drift.Accessions, "、"))
	case errors.Is(err, repository.ErrStocktakeAlreadyClosed):
		return util.Conflict("盘点单已结束")
	case errors.Is(err, repository.ErrStocktakeContainerBusy):
		return util.Conflict("该容器已有进行中的盘点任务，请先完成或取消")
	case errors.Is(err, repository.ErrStocktakeContainerEmpty):
		return util.Conflict("容器内没有在库样本，无法生成盘点清单")
	case errors.Is(err, repository.ErrStocktakeNotComplete):
		return util.Conflict("仍有条目未处理，全部标记完成后才能关单")
	case errors.Is(err, repository.ErrStocktakeTargetInvalid):
		return util.Conflict("目标容器不可用，或位置不符条目缺少新容器与新格位")
	case errors.Is(err, repository.ErrStocktakeTargetDup):
		return util.Conflict("多条位置不符记录指向同一个新格位，请逐支核对后重新标记")
	case errors.Is(err, repository.ErrStocktakePositionBusy):
		return util.Conflict("新格位已被盘点清单外的样本占用")
	case errors.Is(err, repository.ErrStocktakeNoCapacity):
		return util.Conflict("换位后容器占用量将超过容量，请重新选择目标容器")
	case errors.Is(err, repository.ErrStocktakeSamePosition):
		return util.Conflict("新格位与原容器原格位一致，不属于位置不符")
	default:
		return err
	}
}
