package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"biosample-cold-custody-tracking/backend/internal/constants"
)

var stocktakeNumberPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,49}$`)

// StocktakeTask 冻存盘点任务：选择一个可用容器后，把当时在库样本的
// 原容器与格位固化成盘点清单，保管员逐支核对并在关单时一次性换位。
type StocktakeTask struct {
	Base
	TaskNo        string                   `gorm:"size:50;uniqueIndex;not null" json:"taskNo"`
	ContainerID   uint                     `gorm:"index;not null" json:"containerId"`
	Container     StorageContainer         `gorm:"foreignKey:ContainerID" json:"container,omitempty"`
	State         constants.StocktakeState `gorm:"size:20;index;not null;default:'in_progress'" json:"state"`
	TotalCount    int                      `gorm:"not null;default:0" json:"totalCount"`
	PendingCount  int                      `gorm:"not null;default:0" json:"pendingCount"`
	InStockCount  int                      `gorm:"not null;default:0" json:"inStockCount"`
	MissingCount  int                      `gorm:"not null;default:0" json:"missingCount"`
	MismatchCount int                      `gorm:"not null;default:0" json:"mismatchCount"`
	StartedByID   uint                     `gorm:"index;not null" json:"startedById"`
	StartedByName string                   `gorm:"size:100;not null" json:"startedByName"`
	StartedAt     time.Time                `gorm:"index;not null" json:"startedAt"`
	ClosedByID    *uint                    `gorm:"index" json:"closedById,omitempty"`
	ClosedByName  string                   `gorm:"size:100" json:"closedByName,omitempty"`
	ClosedAt      *time.Time               `gorm:"index" json:"closedAt,omitempty"`
	Note          string                   `gorm:"size:1000" json:"note,omitempty"`
	CancelReason  string                   `gorm:"size:1000" json:"cancelReason,omitempty"`
	Items         []StocktakeItem          `gorm:"foreignKey:TaskID" json:"items,omitempty"`
}

func (t *StocktakeTask) Normalize() {
	t.TaskNo = strings.ToUpper(strings.TrimSpace(t.TaskNo))
	t.StartedByName = strings.TrimSpace(t.StartedByName)
	t.ClosedByName = strings.TrimSpace(t.ClosedByName)
	t.Note = strings.TrimSpace(t.Note)
	t.CancelReason = strings.TrimSpace(t.CancelReason)
	if t.State == "" {
		t.State = constants.StocktakeStateInProgress
	}
}

func (t StocktakeTask) Validate() error {
	if !stocktakeNumberPattern.MatchString(t.TaskNo) {
		return fmt.Errorf("stocktake number must contain 3-50 uppercase letters, numbers or hyphens")
	}
	if t.ContainerID == 0 {
		return fmt.Errorf("stocktake container is required")
	}
	if !t.State.Valid() {
		return fmt.Errorf("unsupported stocktake state: %s", t.State)
	}
	if t.StartedByID == 0 || t.StartedByName == "" || t.StartedAt.IsZero() {
		return fmt.Errorf("starter identity and time are required")
	}
	if t.TotalCount < 0 || t.PendingCount < 0 || t.InStockCount < 0 || t.MissingCount < 0 || t.MismatchCount < 0 {
		return fmt.Errorf("stocktake counters cannot be negative")
	}
	if t.PendingCount+t.InStockCount+t.MissingCount+t.MismatchCount != t.TotalCount {
		return fmt.Errorf("stocktake counters do not add up to the total")
	}
	if len([]rune(t.Note)) > 1000 || len([]rune(t.CancelReason)) > 1000 {
		return fmt.Errorf("stocktake note or cancel reason is too long")
	}
	if t.State == constants.StocktakeStateInProgress {
		if t.ClosedAt != nil || t.ClosedByID != nil || t.ClosedByName != "" {
			return fmt.Errorf("open stocktake cannot contain closure metadata")
		}
		return nil
	}
	if t.ClosedAt == nil || t.ClosedByID == nil || *t.ClosedByID == 0 || t.ClosedByName == "" {
		return fmt.Errorf("finished stocktake requires closer identity and time")
	}
	if t.State == constants.StocktakeStateCancelled && t.CancelReason == "" {
		return fmt.Errorf("cancelled stocktake requires a reason")
	}
	return nil
}

// InProgress 用于复合唯一索引：每个容器同时只允许存在一个未结束盘点。
func (t StocktakeTask) InProgress() bool {
	return t.State == constants.StocktakeStateInProgress
}

func (t StocktakeTask) AllResolved() bool {
	return t.State == constants.StocktakeStateInProgress && t.PendingCount == 0 && t.TotalCount > 0
}

func (t StocktakeTask) HasDiscrepancy() bool {
	return t.MissingCount > 0 || t.MismatchCount > 0
}
