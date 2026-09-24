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
)

var (
	ErrStocktakeNotFound       = errors.New("stocktake not found")
	ErrStocktakeAlreadyClosed  = errors.New("stocktake is already finished")
	ErrStocktakeContainerBusy  = errors.New("container already has an open stocktake")
	ErrStocktakeContainerEmpty = errors.New("container has no stored specimens to count")
	ErrStocktakeNotComplete    = errors.New("stocktake still has pending items")
	ErrStocktakeTargetInvalid  = errors.New("stocktake target container is unavailable")
	ErrStocktakeTargetDup      = errors.New("multiple items point to the same new position")
	ErrStocktakePositionBusy   = errors.New("new position is occupied by a specimen outside the stocktake")
	ErrStocktakeNoCapacity     = errors.New("target container does not have enough free capacity")
	ErrStocktakeSpecimenDrift  = errors.New("specimen location changed after the stocktake started")
	ErrStocktakeSamePosition   = errors.New("mislocated item points to its original position")
)

type StocktakeDriftError struct {
	Accessions []string
}

func (e *StocktakeDriftError) Error() string {
	return fmt.Sprintf("specimens drifted since the stocktake started: %s", strings.Join(e.Accessions, ", "))
}

func (e *StocktakeDriftError) Is(target error) bool { return target == ErrStocktakeSpecimenDrift }

type StocktakeFilter struct {
	dto.PageQuery
	State       string `form:"state"`
	ContainerID uint   `form:"containerId"`
}

type StocktakeMark struct {
	Result           constants.StocktakeResult
	Note             string
	NewContainerID   *uint
	NewPosition      string
	NewContainerCode string
	MarkedByID       uint
	MarkedByName     string
	MarkedAt         time.Time
}

type StocktakeResolution struct {
	ClosedByID   uint
	ClosedByName string
	ClosedAt     time.Time
	CancelReason string
}

type StocktakeSpecimenChange struct {
	Before model.Specimen
	After  model.Specimen
}

type StocktakeCloseOutcome struct {
	Stocktake *model.Stocktake
	Changes   []StocktakeSpecimenChange
}

type StocktakeRepository interface {
	List(context.Context, StocktakeFilter) ([]model.Stocktake, int64, error)
	Find(context.Context, uint) (*model.Stocktake, error)
	FindByNumber(context.Context, string) (*model.Stocktake, error)
	CountByMonth(context.Context, string) (int64, error)
	CreateWithSnapshot(context.Context, *model.Stocktake) error
	MarkItem(context.Context, uint, uint, StocktakeMark) (*model.Stocktake, error)
	Close(context.Context, uint, StocktakeResolution) (*StocktakeCloseOutcome, error)
	Cancel(context.Context, uint, StocktakeResolution) (*model.Stocktake, error)
}

type stocktakeRepository struct{ db *gorm.DB }

func NewStocktakeRepository(db *gorm.DB) StocktakeRepository {
	return &stocktakeRepository{db: db}
}

func (r *stocktakeRepository) List(ctx context.Context, filter StocktakeFilter) ([]model.Stocktake, int64, error) {
	query := filter.PageQuery.Normalize()
	db := r.db.WithContext(ctx).Model(&model.Stocktake{})
	if state := strings.TrimSpace(filter.State); state != "" {
		db = db.Where("state = ?", state)
	}
	if filter.ContainerID > 0 {
		db = db.Where("container_id = ?", filter.ContainerID)
	}
	if search := strings.TrimSpace(query.Search); search != "" {
		like := "%" + search + "%"
		db = db.Where("stocktake_no ILIKE ? OR task_month ILIKE ? OR started_by_name ILIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]model.Stocktake, 0)
	err := db.Preload("Container").
		Order("started_at DESC, id DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&items).Error
	return items, total, err
}

func (r *stocktakeRepository) Find(ctx context.Context, id uint) (*model.Stocktake, error) {
	var item model.Stocktake
	err := r.db.WithContext(ctx).
		Preload("Container").
		Preload("Items", func(tx *gorm.DB) *gorm.DB { return tx.Order("origin_position ASC, id ASC") }).
		Preload("Items.Specimen").
		Preload("Items.Specimen.StorageContainer").
		First(&item, id).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *stocktakeRepository) FindByNumber(ctx context.Context, number string) (*model.Stocktake, error) {
	var item model.Stocktake
	err := r.db.WithContext(ctx).Where("stocktake_no = ?", strings.ToUpper(strings.TrimSpace(number))).First(&item).Error
	return &item, err
}

func (r *stocktakeRepository) CountByMonth(ctx context.Context, taskMonth string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Stocktake{}).Where("task_month = ?", strings.TrimSpace(taskMonth)).Count(&count).Error
	return count, err
}

func (r *stocktakeRepository) CreateWithSnapshot(ctx context.Context, task *model.Stocktake) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var container model.StorageContainer
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&container, task.ContainerID).Error; err != nil {
			return err
		}
		if !container.Active || container.Status != "available" {
			return ErrStocktakeTargetInvalid
		}
		var openCount int64
		if err := tx.Model(&model.Stocktake{}).
			Where("container_id = ? AND state = ?", container.ID, constants.StocktakeStateInProgress).
			Count(&openCount).Error; err != nil {
			return err
		}
		if openCount > 0 {
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
		task.TotalItems = len(specimens)
		task.ProcessedItems = 0
		task.InPlaceItems = 0
		task.MissingItems = 0
		task.MislocatedItems = 0
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate stocktake: %w", err)
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		items := make([]model.StocktakeItem, 0, len(specimens))
		for _, specimen := range specimens {
			item := model.StocktakeItem{
				StocktakeID:         task.ID,
				SpecimenID:          specimen.ID,
				OriginContainerID:   container.ID,
				OriginContainerCode: container.Code,
				OriginContainerName: container.Name,
				OriginPosition:      specimen.Position,
				OriginLocation:      locationLabel(container.Location, container.Code, specimen.Position),
				Result:              constants.StocktakeResultPending,
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
		return nil
	})
}

func (r *stocktakeRepository) MarkItem(ctx context.Context, stocktakeID, itemID uint, mark StocktakeMark) (*model.Stocktake, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Stocktake
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, stocktakeID).Error; err != nil {
			return err
		}
		if task.State != constants.StocktakeStateInProgress {
			return ErrStocktakeAlreadyClosed
		}
		var item model.StocktakeItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND stocktake_id = ?", itemID, stocktakeID).First(&item).Error; err != nil {
			return err
		}
		var specimen model.Specimen
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&specimen, item.SpecimenID).Error; err != nil {
			return err
		}
		if !matchesSnapshot(specimen, item) {
			return &StocktakeDriftError{Accessions: []string{specimen.AccessionNo}}
		}
		item.Result = mark.Result
		item.Note = strings.TrimSpace(mark.Note)
		item.NewContainerID = nil
		item.NewContainerCode = ""
		item.NewPosition = ""
		if mark.Result == constants.StocktakeResultMislocated {
			if mark.NewContainerID == nil || *mark.NewContainerID == 0 || strings.TrimSpace(mark.NewPosition) == "" {
				return ErrStocktakeTargetInvalid
			}
			if *mark.NewContainerID == item.OriginContainerID && strings.TrimSpace(mark.NewPosition) == item.OriginPosition {
				return ErrStocktakeSamePosition
			}
			var target model.StorageContainer
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, *mark.NewContainerID).Error; err != nil {
				return err
			}
			if !target.Active || target.Status != "available" {
				return ErrStocktakeTargetInvalid
			}
			item.NewContainerID = &target.ID
			item.NewContainerCode = target.Code
			item.NewPosition = strings.TrimSpace(mark.NewPosition)
		}
		item.MarkedByID = &mark.MarkedByID
		item.MarkedByName = strings.TrimSpace(mark.MarkedByName)
		item.MarkedAt = &mark.MarkedAt
		item.Normalize()
		if err := item.Validate(); err != nil {
			return fmt.Errorf("validate stocktake item: %w", err)
		}
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		return recountStocktake(tx, stocktakeID)
	})
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, stocktakeID)
}

func (r *stocktakeRepository) Close(ctx context.Context, stocktakeID uint, resolution StocktakeResolution) (*StocktakeCloseOutcome, error) {
	outcome := &StocktakeCloseOutcome{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Stocktake
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, stocktakeID).Error; err != nil {
			return err
		}
		if task.State != constants.StocktakeStateInProgress {
			return ErrStocktakeAlreadyClosed
		}
		var items []model.StocktakeItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("stocktake_id = ?", stocktakeID).Order("id ASC").Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrStocktakeNotFound
		}
		for _, item := range items {
			if item.Result == constants.StocktakeResultPending {
				return ErrStocktakeNotComplete
			}
		}
		specimenIDs := make([]uint, 0, len(items))
		for _, item := range items {
			specimenIDs = append(specimenIDs, item.SpecimenID)
		}
		var lockedSpecimens []model.Specimen
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", specimenIDs).Order("id ASC").Find(&lockedSpecimens).Error; err != nil {
			return err
		}
		specimens := make(map[uint]model.Specimen, len(lockedSpecimens))
		for _, specimen := range lockedSpecimens {
			specimens[specimen.ID] = specimen
		}
		drifted := make([]string, 0)
		for _, item := range items {
			specimen, ok := specimens[item.SpecimenID]
			if !ok || !matchesSnapshot(specimen, item) {
				if ok {
					drifted = append(drifted, specimen.AccessionNo)
				}
			}
		}
		if len(drifted) > 0 {
			return &StocktakeDriftError{Accessions: drifted}
		}
		containerIDSet := make(map[uint]struct{})
		type destinationKey struct {
			containerID uint
			position    string
		}
		destinationKeys := make(map[destinationKey]uint)
		for _, item := range items {
			containerIDSet[item.OriginContainerID] = struct{}{}
			if item.Result == constants.StocktakeResultMislocated && item.NewContainerID != nil {
				containerIDSet[*item.NewContainerID] = struct{}{}
				key := destinationKey{containerID: *item.NewContainerID, position: item.NewPosition}
				if _, exists := destinationKeys[key]; exists {
					return ErrStocktakeTargetDup
				}
				destinationKeys[key] = item.SpecimenID
			}
		}
		containerIDs := make([]uint, 0, len(containerIDSet))
		for id := range containerIDSet {
			containerIDs = append(containerIDs, id)
		}
		var lockedContainers []model.StorageContainer
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN ?", containerIDs).Order("id ASC").Find(&lockedContainers).Error; err != nil {
			return err
		}
		containers := make(map[uint]model.StorageContainer, len(lockedContainers))
		for _, container := range lockedContainers {
			containers[container.ID] = container
		}
		vacatingIDs := make([]uint, 0)
		deltas := make(map[uint]int)
		for _, item := range items {
			switch item.Result {
			case constants.StocktakeResultMissing:
				vacatingIDs = append(vacatingIDs, item.SpecimenID)
				deltas[item.OriginContainerID]--
			case constants.StocktakeResultMislocated:
				if item.NewContainerID == nil {
					return ErrStocktakeTargetInvalid
				}
				target, ok := containers[*item.NewContainerID]
				if !ok || !target.Active || target.Status != "available" {
					return ErrStocktakeTargetInvalid
				}
				vacatingIDs = append(vacatingIDs, item.SpecimenID)
				deltas[item.OriginContainerID]--
				deltas[*item.NewContainerID]++
			}
		}
		for containerID, delta := range deltas {
			container := containers[containerID]
			if container.Occupied+delta < 0 || container.Occupied+delta > container.Capacity {
				return ErrStocktakeNoCapacity
			}
		}
		for key, specimenID := range destinationKeys {
			var collisions int64
			query := tx.Model(&model.Specimen{}).
				Where("storage_container_id = ? AND position = ? AND id <> ? AND state NOT IN ?",
					key.containerID, key.position, specimenID,
					[]constants.SpecimenState{constants.SpecimenStateReleased, constants.SpecimenStateDisposed})
			if len(vacatingIDs) > 0 {
				query = query.Where("id NOT IN ?", vacatingIDs)
			}
			if err := query.Count(&collisions).Error; err != nil {
				return err
			}
			if collisions > 0 {
				return ErrStocktakePositionBusy
			}
		}
		changes := make([]StocktakeSpecimenChange, 0)
		for _, item := range items {
			specimen := specimens[item.SpecimenID]
			before := specimen
			switch item.Result {
			case constants.StocktakeResultInPlace:
				continue
			case constants.StocktakeResultMissing:
				specimen.StorageContainerID = nil
				specimen.StorageContainer = nil
				specimen.Position = ""
				specimen.State = constants.SpecimenStateDisposed
				specimen.Notes = truncateRunes(strings.TrimSpace(specimen.Notes+"\n盘点缺失: "+item.Note), 1000)
			case constants.StocktakeResultMislocated:
				target := containers[*item.NewContainerID]
				specimen.StorageContainerID = &target.ID
				targetCopy := target
				specimen.StorageContainer = &targetCopy
				specimen.Position = item.NewPosition
				specimen.State = constants.SpecimenStateStored
			}
			specimen.Normalize()
			if err := specimen.Validate(); err != nil {
				return fmt.Errorf("validate counted specimen: %w", err)
			}
			if err := tx.Save(&specimen).Error; err != nil {
				return err
			}
			changes = append(changes, StocktakeSpecimenChange{Before: before, After: specimen})
		}
		for containerID, delta := range deltas {
			if delta == 0 {
				continue
			}
			result := tx.Model(&model.StorageContainer{}).Where("id = ?", containerID).
				UpdateColumn("occupied", gorm.Expr("GREATEST(occupied + ?, 0)", delta))
			if result.Error != nil {
				return result.Error
			}
		}
		task.State = constants.StocktakeStateClosed
		task.ClosedByID = &resolution.ClosedByID
		task.ClosedByName = strings.TrimSpace(resolution.ClosedByName)
		task.ClosedAt = &resolution.ClosedAt
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate closed stocktake: %w", err)
		}
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		outcome.Changes = changes
		return nil
	})
	if err != nil {
		return nil, err
	}
	closed, err := r.Find(ctx, stocktakeID)
	if err != nil {
		return nil, err
	}
	outcome.Stocktake = closed
	return outcome, nil
}

func (r *stocktakeRepository) Cancel(ctx context.Context, stocktakeID uint, resolution StocktakeResolution) (*model.Stocktake, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Stocktake
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, stocktakeID).Error; err != nil {
			return err
		}
		if task.State != constants.StocktakeStateInProgress {
			return ErrStocktakeAlreadyClosed
		}
		task.State = constants.StocktakeStateCancelled
		task.CancelReason = strings.TrimSpace(resolution.CancelReason)
		task.ClosedByID = &resolution.ClosedByID
		task.ClosedByName = strings.TrimSpace(resolution.ClosedByName)
		task.ClosedAt = &resolution.ClosedAt
		task.Normalize()
		if err := task.Validate(); err != nil {
			return fmt.Errorf("validate cancelled stocktake: %w", err)
		}
		return tx.Save(&task).Error
	})
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, stocktakeID)
}

func matchesSnapshot(specimen model.Specimen, item model.StocktakeItem) bool {
	if specimen.State.Terminal() {
		return false
	}
	if specimen.StorageContainerID == nil || *specimen.StorageContainerID != item.OriginContainerID {
		return false
	}
	return specimen.Position == item.OriginPosition
}

func recountStocktake(tx *gorm.DB, stocktakeID uint) error {
	var task model.Stocktake
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, stocktakeID).Error; err != nil {
		return err
	}
	var rows []struct {
		Result constants.StocktakeResult
		Count  int
	}
	if err := tx.Model(&model.StocktakeItem{}).
		Select("result, count(*) AS count").Where("stocktake_id = ?", stocktakeID).
		Group("result").Scan(&rows).Error; err != nil {
		return err
	}
	task.ProcessedItems = 0
	task.InPlaceItems = 0
	task.MissingItems = 0
	task.MislocatedItems = 0
	for _, row := range rows {
		switch row.Result {
		case constants.StocktakeResultInPlace:
			task.InPlaceItems = row.Count
		case constants.StocktakeResultMissing:
			task.MissingItems = row.Count
		case constants.StocktakeResultMislocated:
			task.MislocatedItems = row.Count
		}
	}
	task.ProcessedItems = task.InPlaceItems + task.MissingItems + task.MislocatedItems
	task.Normalize()
	if err := task.Validate(); err != nil {
		return fmt.Errorf("validate stocktake counters: %w", err)
	}
	return tx.Save(&task).Error
}

func locationLabel(location, code, position string) string {
	return strings.Trim(strings.Join([]string{strings.TrimSpace(location), strings.TrimSpace(code), strings.TrimSpace(position)}, " / "), " / ")
}

// truncateRunes caps a string to at most limit runes, guarding size-limited text columns.
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
