package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"biosample-cold-custody-tracking/backend/internal/constants"
	"biosample-cold-custody-tracking/backend/internal/dto"
	"biosample-cold-custody-tracking/backend/internal/model"
	"biosample-cold-custody-tracking/backend/internal/util"
)

var (
	ErrStocktakeFinished          = errors.New("stocktake task is already finished")
	ErrStocktakeContainerBusy     = errors.New("container already has an in-progress stocktake")
	ErrStocktakeContainerUnusable = errors.New("stocktake container is missing or not available")
	ErrStocktakeContainerEmpty    = errors.New("stocktake container has no in-storage specimens")
	ErrStocktakeHasPending        = errors.New("stocktake still has pending items")
	ErrStocktakeItemDrifted       = errors.New("specimen moved or was handed over after the stocktake started")
	ErrStocktakeSlotOccupied      = errors.New("new stocktake position is already occupied")
	ErrStocktakeTargetFull        = errors.New("new stocktake container is not available or is full")
	ErrStocktakeNewPositionSame   = errors.New("mismatched item new position must differ from the original position")
	ErrStocktakeTaskNoTaken       = errors.New("stocktake number already exists")
)

type StocktakeFilter struct {
	dto.PageQuery
	State       string `form:"state"`
	ContainerID uint   `form:"containerId"`
}

type StocktakeItemFilter struct {
	dto.PageQuery
	Result  string `form:"result"`
	Pending bool   `form:"pending"`
}

type StocktakeItemMark struct {
	Result         constants.StocktakeResult
	Remark         string
	NewContainerID *uint
	NewPosition    string
	CheckedByID    uint
	CheckedByName  string
	CheckedAt      time.Time
}

type StocktakeRepository interface {
	List(context.Context, StocktakeFilter) ([]model.StocktakeTask, int64, error)
	Find(context.Context, uint) (*model.StocktakeTask, error)
	FindByNumber(context.Context, string) (*model.StocktakeTask, error)
	Create(context.Context, *model.StocktakeTask) error
	ListItems(context.Context, uint, StocktakeItemFilter) ([]model.StocktakeItem, int64, error)
	FindItem(context.Context, uint) (*model.StocktakeItem, error)
	MarkItem(context.Context, uint, StocktakeItemMark) (*model.StocktakeItem, *model.StocktakeTask, model.StocktakeItem, error)
	Close(context.Context, uint, uint, string, time.Time) (*model.StocktakeTask, []model.Specimen, []model.Specimen, []model.StorageContainer, error)
	Cancel(context.Context, uint, uint, string, string, time.Time) (*model.StocktakeTask, error)
}

type stocktakeRepository struct{ db *gorm.DB }

func NewStocktakeRepository(db *gorm.DB) StocktakeRepository {
	return &stocktakeRepository{db: db}
}

func (r *stocktakeRepository) List(ctx context.Context, filter StocktakeFilter) ([]model.StocktakeTask, int64, error) {
	query := filter.PageQuery.Normalize()
	db := r.db.WithContext(ctx).Model(&model.StocktakeTask{})
	if state := strings.TrimSpace(filter.State); state != "" {
		db = db.Where("state = ?", state)
	}
	if filter.ContainerID > 0 {
		db = db.Where("container_id = ?", filter.ContainerID)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + search + "%"
		db = db.Where("task_no ILIKE ? OR started_by_name ILIKE ? OR closed_by_name ILIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]model.StocktakeTask, 0)
	err := db.Preload("Container").
		Order("started_at DESC, id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error
	return items, total, err
}

func (r *stocktakeRepository) Find(ctx context.Context, id uint) (*model.StocktakeTask, error) {
	var task model.StocktakeTask
	err := r.db.WithContext(ctx).Preload("Container").First(&task, id).Error
	return &task, err
}

func (r *stocktakeRepository) FindByNumber(ctx context.Context, number string) (*model.StocktakeTask, error) {
	var task model.StocktakeTask
	err := r.db.WithContext(ctx).Where("task_no = ?", strings.ToUpper(strings.TrimSpace(number))).First(&task).Error
	return &task, err
}

func (r *stocktakeRepository) Create(ctx context.Context, task *model.StocktakeTask) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var container model.StorageContainer
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&container, task.ContainerID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStocktakeContainerUnusable
			}
			return err
		}
		if !container.Active || container.Status != "available" {
			return ErrStocktakeContainerUnusable
		}
		var activeCount int64
		if err := tx.Model(&model.StocktakeTask{}).
			Where("container_id = ? AND state = ?", container.ID, constants.StocktakeStateInProgress).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrStocktakeContainerBusy
		}
		var specimens []model.Specimen
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("storage_container_id = ? AND state NOT IN ?", container.ID, []constants.SpecimenState{constants.SpecimenStateReleased, constants.SpecimenStateDisposed}).
			Order("position ASC, id ASC").Find(&specimens).Error; err != nil {
			return err
		}
		if len(specimens) == 0 {
			return ErrStocktakeContainerEmpty
		}
		task.ContainerID = container.ID
		task.TotalCount = len(specimens)
		task.PendingCount = len(specimens)
		task.InStockCount = 0
		task.MissingCount = 0
		task.MismatchCount = 0
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate stocktake task: %w", err)
		}
		if err := tx.Create(task).Error; err != nil {
			if util.IsUniqueViolation(err) {
				return ErrStocktakeTaskNoTaken
			}
			return err
		}
		items := make([]model.StocktakeItem, 0, len(specimens))
		for _, specimen := range specimens {
			item := model.StocktakeItem{
				TaskID:            task.ID,
				SpecimenID:        specimen.ID,
				AccessionNo:       specimen.AccessionNo,
				SampleType:        specimen.SampleType,
				OriginContainerID: container.ID,
				OriginPosition:    specimen.Position,
				OriginCustodian:   specimen.CurrentCustodian,
			}
			item.Normalize()
			if err := item.Validate(); err != nil {
				return fmt.Errorf("validate stocktake item snapshot: %w", err)
			}
			items = append(items, item)
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		task.Container = container
		task.Items = items
		return nil
	})
}

func (r *stocktakeRepository) ListItems(ctx context.Context, taskID uint, filter StocktakeItemFilter) ([]model.StocktakeItem, int64, error) {
	query := filter.PageQuery.Normalize()
	db := r.db.WithContext(ctx).Model(&model.StocktakeItem{}).Where("task_id = ?", taskID)
	if filter.Pending {
		db = db.Where("result = ?", "")
	} else if result := strings.TrimSpace(filter.Result); result != "" {
		db = db.Where("result = ?", result)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + search + "%"
		db = db.Where("accession_no ILIKE ? OR sample_type ILIKE ? OR origin_position ILIKE ? OR new_position ILIKE ? OR remark ILIKE ?", like, like, like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]model.StocktakeItem, 0)
	err := db.Preload("Specimen").Preload("Specimen.StorageContainer").
		Preload("OriginContainer").Preload("NewContainer").
		Order("origin_position ASC, id ASC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error
	return items, total, err
}

func (r *stocktakeRepository) FindItem(ctx context.Context, id uint) (*model.StocktakeItem, error) {
	var item model.StocktakeItem
	err := r.db.WithContext(ctx).
		Preload("Task").Preload("Task.Container").
		Preload("Specimen").Preload("Specimen.StorageContainer").
		Preload("OriginContainer").Preload("NewContainer").First(&item, id).Error
	return &item, err
}

func recountTaskCounters(tx *gorm.DB, taskID uint) (model.StocktakeTask, error) {
	var task model.StocktakeTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error; err != nil {
		return task, err
	}
	var grouped []struct {
		Result string
		Count  int
	}
	if err := tx.Model(&model.StocktakeItem{}).Select("COALESCE(result, '') AS result, count(*) AS count").Where("task_id = ?", taskID).Group("result").Scan(&grouped).Error; err != nil {
		return task, err
	}
	task.TotalCount, task.PendingCount, task.InStockCount, task.MissingCount, task.MismatchCount = 0, 0, 0, 0, 0
	for _, row := range grouped {
		task.TotalCount += row.Count
		switch constants.StocktakeResult(row.Result) {
		case constants.StocktakeResultInStock:
			task.InStockCount += row.Count
		case constants.StocktakeResultMissing:
			task.MissingCount += row.Count
		case constants.StocktakeResultMismatched:
			task.MismatchCount += row.Count
		default:
			task.PendingCount += row.Count
		}
	}
	if err := task.Validate(); err != nil {
		return task, fmt.Errorf("validate recounted stocktake: %w", err)
	}
	if err := tx.Save(&task).Error; err != nil {
		return task, err
	}
	return task, nil
}

func (r *stocktakeRepository) MarkItem(ctx context.Context, itemID uint, mark StocktakeItemMark) (*model.StocktakeItem, *model.StocktakeTask, model.StocktakeItem, error) {
	var before model.StocktakeItem
	var savedItem model.StocktakeItem
	var savedTask model.StocktakeTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&savedItem, itemID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&savedTask, savedItem.TaskID).Error; err != nil {
			return err
		}
		if savedTask.State != constants.StocktakeStateInProgress {
			return ErrStocktakeFinished
		}
		before = savedItem
		newContainerID := mark.NewContainerID
		newPosition := strings.TrimSpace(mark.NewPosition)
		if mark.Result == constants.StocktakeResultMismatched {
			if newContainerID == nil || *newContainerID == 0 || newPosition == "" {
				return ErrStocktakeTargetFull
			}
			if *newContainerID == savedItem.OriginContainerID && newPosition == savedItem.OriginPosition {
				return ErrStocktakeNewPositionSame
			}
			var target model.StorageContainer
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, *newContainerID).Error; err != nil {
				return err
			}
			if !target.Active || target.Status != "available" {
				return ErrStocktakeTargetFull
			}
			// 现网格位是否已被其他在库样本占用。
			var occupied int64
			if err := tx.Model(&model.Specimen{}).
				Where("storage_container_id = ? AND position = ? AND id <> ? AND state NOT IN ?", target.ID, newPosition, savedItem.SpecimenID, []constants.SpecimenState{constants.SpecimenStateReleased, constants.SpecimenStateDisposed}).
				Count(&occupied).Error; err != nil {
				return err
			}
			if occupied > 0 {
				return ErrStocktakeSlotOccupied
			}
			// 同一任务中另一支位置不符样本不能选同一目标格位。
			var sibling int64
			if err := tx.Model(&model.StocktakeItem{}).
				Where("task_id = ? AND id <> ? AND result = ? AND new_container_id = ? AND new_position = ?", savedTask.ID, savedItem.ID, constants.StocktakeResultMismatched, target.ID, newPosition).
				Count(&sibling).Error; err != nil {
				return err
			}
			if sibling > 0 {
				return ErrStocktakeSlotOccupied
			}
		} else {
			newContainerID = nil
			newPosition = ""
		}
		now := mark.CheckedAt
		savedItem.Result = mark.Result
		savedItem.Remark = strings.TrimSpace(mark.Remark)
		savedItem.NewContainerID = newContainerID
		savedItem.NewPosition = newPosition
		savedItem.CheckedByID = &mark.CheckedByID
		savedItem.CheckedByName = strings.TrimSpace(mark.CheckedByName)
		savedItem.CheckedAt = &now
		savedItem.Normalize()
		if err := savedItem.Validate(); err != nil {
			return fmt.Errorf("validate marked item: %w", err)
		}
		if err := tx.Save(&savedItem).Error; err != nil {
			return err
		}
		recounted, err := recountTaskCounters(tx, savedTask.ID)
		if err != nil {
			return err
		}
		savedTask = recounted
		return nil
	})
	if err != nil {
		return nil, nil, model.StocktakeItem{}, err
	}
	return &savedItem, &savedTask, before, nil
}

func (r *stocktakeRepository) Close(ctx context.Context, taskID, closerID uint, closerName string, closedAt time.Time) (*model.StocktakeTask, []model.Specimen, []model.Specimen, []model.StorageContainer, error) {
	var task model.StocktakeTask
	beforeSpecimens := make([]model.Specimen, 0)
	afterSpecimens := make([]model.Specimen, 0)
	changedContainers := make([]model.StorageContainer, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.State != constants.StocktakeStateInProgress {
			return ErrStocktakeFinished
		}
		var items []model.StocktakeItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).Order("id ASC").Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrStocktakeHasPending
		}
		movers := make([]model.StocktakeItem, 0)
		for _, item := range items {
			if item.Result == "" {
				return ErrStocktakeHasPending
			}
			if item.Result == constants.StocktakeResultMismatched {
				movers = append(movers, item)
			}
		}
		containerLocks := map[uint]struct{}{task.ContainerID: {}}
		for _, item := range movers {
			if item.NewContainerID != nil {
				containerLocks[*item.NewContainerID] = struct{}{}
			}
		}
		lockedContainers := map[uint]model.StorageContainer{}
		for containerID := range containerLocks {
			var locked model.StorageContainer
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, containerID).Error; err != nil {
				return err
			}
			lockedContainers[containerID] = locked
		}
		// 关单前逐支复核：盘点开始后发生过交接/换位的旧清单不允许覆盖现位置。
		for _, item := range items {
			var specimen model.Specimen
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&specimen, item.SpecimenID).Error; err != nil {
				return err
			}
			beforeSpecimens = append(beforeSpecimens, specimen)
			if specimen.StorageContainerID == nil || *specimen.StorageContainerID != item.OriginContainerID ||
				specimen.Position != item.OriginPosition || specimen.CurrentCustodian != item.OriginCustodian ||
				specimen.State.Terminal() {
				return fmt.Errorf("%w: %s", ErrStocktakeItemDrifted, item.AccessionNo)
			}
		}
		// 目标容器容量预检（含同一任务内的净增减），并复查目标格位仍空闲。
		deltas := map[uint]int{}
		targetSlots := map[[2]any]uint{}
		for _, item := range movers {
			targetID := *item.NewContainerID
			if targetID != task.ContainerID {
				deltas[task.ContainerID]--
				deltas[targetID]++
			}
			key := [2]any{targetID, item.NewPosition}
			if _, exists := targetSlots[key]; exists {
				return ErrStocktakeSlotOccupied
			}
			targetSlots[key] = item.SpecimenID
		}
		for containerID, delta := range deltas {
			c := lockedContainers[containerID]
			if delta > 0 && (!c.Active || c.Status != "available" || c.Occupied+delta > c.Capacity) {
				return ErrStocktakeTargetFull
			}
		}
		for key, specimenID := range targetSlots {
			containerID := key[0].(uint)
			position := key[1].(string)
			var occupied int64
			if err := tx.Model(&model.Specimen{}).
				Where("storage_container_id = ? AND position = ? AND id <> ? AND state NOT IN ?", containerID, position, specimenID, []constants.SpecimenState{constants.SpecimenStateReleased, constants.SpecimenStateDisposed}).
				Count(&occupied).Error; err != nil {
				return err
			}
			if occupied > 0 {
				return ErrStocktakeSlotOccupied
			}
		}
		// 一次性换位：仅位置不符样本移动，在库与缺失样本保持原容器注册。
		for _, item := range movers {
			var specimen model.Specimen
			if err := tx.First(&specimen, item.SpecimenID).Error; err != nil {
				return err
			}
			specimen.StorageContainerID = item.NewContainerID
			specimen.Position = item.NewPosition
			if err := specimen.Validate(); err != nil {
				return fmt.Errorf("validate relocated specimen: %w", err)
			}
			if err := tx.Save(&specimen).Error; err != nil {
				return err
			}
			afterSpecimens = append(afterSpecimens, specimen)
		}
		// 调整两边容器占用量（原容器 -N，目标容器 +N，同容器内移动不重复计数）。
		for containerID, delta := range deltas {
			if delta == 0 {
				continue
			}
			expression := gorm.Expr("occupied + ?", delta)
			if delta < 0 {
				expression = gorm.Expr("GREATEST(occupied + ?, 0)", delta)
			}
			result := tx.Model(&model.StorageContainer{}).Where("id = ?", containerID).UpdateColumn("occupied", expression)
			if result.Error != nil {
				return result.Error
			}
		}
		for containerID := range containerLocks {
			var c model.StorageContainer
			if err := tx.First(&c, containerID).Error; err != nil {
				return err
			}
			if err := c.Validate(); err != nil {
				return fmt.Errorf("validate container occupancy: %w", err)
			}
			changedContainers = append(changedContainers, c)
		}
		task.State = constants.StocktakeStateClosed
		task.ClosedByID = &closerID
		task.ClosedByName = strings.TrimSpace(closerName)
		task.ClosedAt = &closedAt
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate closed stocktake: %w", err)
		}
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	closed, err := r.Find(ctx, task.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return closed, beforeSpecimens, afterSpecimens, changedContainers, nil
}

func (r *stocktakeRepository) Cancel(ctx context.Context, taskID, userID uint, userName, reason string, cancelledAt time.Time) (*model.StocktakeTask, error) {
	var task model.StocktakeTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.State != constants.StocktakeStateInProgress {
			return ErrStocktakeFinished
		}
		task.State = constants.StocktakeStateCancelled
		task.CancelReason = strings.TrimSpace(reason)
		task.ClosedByID = &userID
		task.ClosedByName = strings.TrimSpace(userName)
		task.ClosedAt = &cancelledAt
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate cancelled stocktake: %w", err)
		}
		return tx.Save(&task).Error
	})
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, task.ID)
}
