package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"biosample-cold-custody-tracking/backend/internal/constants"
)

var stocktakeNumberPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,49}$`)

type Stocktake struct {
	Base
	StocktakeNo     string                   `gorm:"size:50;uniqueIndex;not null" json:"stocktakeNo"`
	TaskMonth       string                   `gorm:"size:7;index;not null" json:"taskMonth"`
	ContainerID     uint                     `gorm:"index;not null" json:"containerId"`
	Container       *StorageContainer        `json:"container,omitempty"`
	State           constants.StocktakeState `gorm:"size:20;index;not null;default:'in_progress'" json:"state"`
	TotalItems      int                      `gorm:"not null;default:0" json:"totalItems"`
	ProcessedItems  int                      `gorm:"not null;default:0" json:"processedItems"`
	InPlaceItems    int                      `gorm:"not null;default:0" json:"inPlaceItems"`
	MissingItems    int                      `gorm:"not null;default:0" json:"missingItems"`
	MislocatedItems int                      `gorm:"not null;default:0" json:"mislocatedItems"`
	StartedByID     uint                     `gorm:"index;not null" json:"startedById"`
	StartedByName   string                   `gorm:"size:100;not null" json:"startedByName"`
	StartedAt       time.Time                `gorm:"index;not null" json:"startedAt"`
	ClosedByID      *uint                    `gorm:"index" json:"closedById,omitempty"`
	ClosedByName    string                   `gorm:"size:100" json:"closedByName,omitempty"`
	ClosedAt        *time.Time               `gorm:"index" json:"closedAt,omitempty"`
	CancelReason    string                   `gorm:"size:1000" json:"cancelReason,omitempty"`
	Items           []StocktakeItem          `json:"items,omitempty"`
}

func (s *Stocktake) Normalize() {
	s.StocktakeNo = strings.ToUpper(strings.TrimSpace(s.StocktakeNo))
	s.TaskMonth = strings.TrimSpace(s.TaskMonth)
	s.StartedByName = strings.TrimSpace(s.StartedByName)
	s.ClosedByName = strings.TrimSpace(s.ClosedByName)
	s.CancelReason = strings.TrimSpace(s.CancelReason)
	if s.State == "" {
		s.State = constants.StocktakeStateInProgress
	}
}

func (s Stocktake) Validate() error {
	if !stocktakeNumberPattern.MatchString(s.StocktakeNo) {
		return fmt.Errorf("stocktake number must contain 3-50 uppercase letters, numbers or hyphens")
	}
	if len(s.TaskMonth) != 7 || s.TaskMonth[4] != '-' {
		return fmt.Errorf("task month must use the YYYY-MM format")
	}
	if s.ContainerID == 0 {
		return fmt.Errorf("stocktake container is required")
	}
	if !s.State.Valid() {
		return fmt.Errorf("unsupported stocktake state: %s", s.State)
	}
	if s.StartedByID == 0 || s.StartedByName == "" || s.StartedAt.IsZero() {
		return fmt.Errorf("starter identity and time are required")
	}
	if s.TotalItems < 0 || s.ProcessedItems < 0 || s.InPlaceItems < 0 || s.MissingItems < 0 || s.MislocatedItems < 0 {
		return fmt.Errorf("stocktake counters must not be negative")
	}
	if s.ProcessedItems != s.InPlaceItems+s.MissingItems+s.MislocatedItems {
		return fmt.Errorf("processed items must equal the sum of in-place, missing and mislocated items")
	}
	if s.ProcessedItems > s.TotalItems {
		return fmt.Errorf("processed items must not exceed total items")
	}
	if len([]rune(s.StartedByName)) > 100 || len([]rune(s.ClosedByName)) > 100 {
		return fmt.Errorf("stocktake operator name cannot exceed 100 characters")
	}
	if len([]rune(s.CancelReason)) > 1000 {
		return fmt.Errorf("cancel reason cannot exceed 1000 characters")
	}
	if s.State == constants.StocktakeStateInProgress {
		if s.ClosedAt != nil || s.ClosedByID != nil || s.ClosedByName != "" {
			return fmt.Errorf("open stocktake cannot contain closing metadata")
		}
	} else {
		if s.ClosedAt == nil || s.ClosedByID == nil || *s.ClosedByID == 0 || s.ClosedByName == "" {
			return fmt.Errorf("finished stocktake requires closer identity and time")
		}
	}
	if s.State == constants.StocktakeStateCancelled && s.CancelReason == "" {
		return fmt.Errorf("cancelled stocktake requires a reason")
	}
	return nil
}

func (s Stocktake) ProgressPercent() int {
	if s.TotalItems == 0 {
		return 0
	}
	return s.ProcessedItems * 100 / s.TotalItems
}

func (s Stocktake) AllProcessed() bool {
	return s.TotalItems > 0 && s.ProcessedItems == s.TotalItems
}

type StocktakeItem struct {
	Base
	StocktakeID         uint                      `gorm:"index:idx_stocktake_item,unique;not null" json:"stocktakeId"`
	Stocktake           *Stocktake                `json:"stocktake,omitempty"`
	SpecimenID          uint                      `gorm:"index:idx_stocktake_item,unique;not null" json:"specimenId"`
	Specimen            *Specimen                 `json:"specimen,omitempty"`
	OriginContainerID   uint                      `gorm:"index;not null" json:"originContainerId"`
	OriginContainerCode string                    `gorm:"size:32;not null" json:"originContainerCode"`
	OriginContainerName string                    `gorm:"size:100;not null" json:"originContainerName"`
	OriginPosition      string                    `gorm:"size:120;not null" json:"originPosition"`
	OriginLocation      string                    `gorm:"size:200;not null" json:"originLocation"`
	Result              constants.StocktakeResult `gorm:"size:20;index;not null;default:'pending'" json:"result"`
	Note                string                    `gorm:"size:1000" json:"note,omitempty"`
	NewContainerID      *uint                     `gorm:"index" json:"newContainerId,omitempty"`
	NewContainerCode    string                    `gorm:"size:32" json:"newContainerCode,omitempty"`
	NewPosition         string                    `gorm:"size:120" json:"newPosition,omitempty"`
	MarkedByID          *uint                     `gorm:"index" json:"markedById,omitempty"`
	MarkedByName        string                    `gorm:"size:100" json:"markedByName,omitempty"`
	MarkedAt            *time.Time                `gorm:"index" json:"markedAt,omitempty"`
}

func (i *StocktakeItem) Normalize() {
	i.OriginContainerCode = strings.ToUpper(strings.TrimSpace(i.OriginContainerCode))
	i.OriginContainerName = strings.TrimSpace(i.OriginContainerName)
	i.OriginPosition = strings.TrimSpace(i.OriginPosition)
	i.OriginLocation = strings.TrimSpace(i.OriginLocation)
	i.Note = strings.TrimSpace(i.Note)
	i.NewContainerCode = strings.ToUpper(strings.TrimSpace(i.NewContainerCode))
	i.NewPosition = strings.TrimSpace(i.NewPosition)
	i.MarkedByName = strings.TrimSpace(i.MarkedByName)
	if i.Result == "" {
		i.Result = constants.StocktakeResultPending
	}
}

func (i StocktakeItem) Validate() error {
	if i.StocktakeID == 0 || i.SpecimenID == 0 {
		return fmt.Errorf("stocktake item must belong to a stocktake and a specimen")
	}
	if i.OriginContainerID == 0 || i.OriginContainerCode == "" || i.OriginPosition == "" {
		return fmt.Errorf("origin container and position snapshot is required")
	}
	if !i.Result.Valid() {
		return fmt.Errorf("unsupported stocktake result: %s", i.Result)
	}
	if len([]rune(i.OriginPosition)) > 120 || len([]rune(i.NewPosition)) > 120 {
		return fmt.Errorf("stocktake position cannot exceed 120 characters")
	}
	if len([]rune(i.Note)) > 1000 {
		return fmt.Errorf("stocktake note cannot exceed 1000 characters")
	}
	if i.Result == constants.StocktakeResultPending {
		if i.Note != "" || i.NewContainerID != nil || i.NewPosition != "" || i.MarkedAt != nil || i.MarkedByID != nil {
			return fmt.Errorf("pending item cannot carry a resolution")
		}
		return nil
	}
	if i.MarkedByID == nil || *i.MarkedByID == 0 || i.MarkedByName == "" || i.MarkedAt == nil {
		return fmt.Errorf("resolved item requires marker identity and time")
	}
	if i.Result == constants.StocktakeResultMissing && i.Note == "" {
		return fmt.Errorf("missing specimen requires a note")
	}
	if i.Result == constants.StocktakeResultMislocated {
		if i.NewContainerID == nil || *i.NewContainerID == 0 || i.NewContainerCode == "" || i.NewPosition == "" {
			return fmt.Errorf("mislocated specimen requires a new container and position")
		}
	}
	if i.Result != constants.StocktakeResultMislocated {
		if i.NewContainerID != nil || i.NewContainerCode != "" || i.NewPosition != "" {
			return fmt.Errorf("only mislocated specimens carry a relocation target")
		}
	}
	return nil
}
