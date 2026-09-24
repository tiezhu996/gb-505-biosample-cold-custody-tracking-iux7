package model

import (
	"fmt"
	"strings"
	"time"

	"biosample-cold-custody-tracking/backend/internal/constants"
)

// StocktakeItem 固化盘点开始那一刻样本的原容器与格位；核对结果逐支标记，
// 关单前不回写样本位置。
type StocktakeItem struct {
	Base
	TaskID            uint                      `gorm:"uniqueIndex:idx_stocktake_item_specimen;index;not null" json:"taskId"`
	Task              *StocktakeTask            `gorm:"foreignKey:TaskID" json:"task,omitempty"`
	SpecimenID        uint                      `gorm:"uniqueIndex:idx_stocktake_item_specimen;index;not null" json:"specimenId"`
	Specimen          Specimen                  `gorm:"foreignKey:SpecimenID" json:"specimen,omitempty"`
	AccessionNo       string                    `gorm:"size:50;index;not null" json:"accessionNo"`
	SampleType        string                    `gorm:"size:100;not null" json:"sampleType"`
	OriginContainerID uint                      `gorm:"index;not null" json:"originContainerId"`
	OriginContainer   *StorageContainer         `gorm:"foreignKey:OriginContainerID" json:"originContainer,omitempty"`
	OriginPosition    string                    `gorm:"size:120;not null" json:"originPosition"`
	OriginCustodian   string                    `gorm:"size:100;not null" json:"originCustodian"`
	Result            constants.StocktakeResult `gorm:"size:20;index;not null;default:''" json:"result"`
	Remark            string                    `gorm:"size:1000" json:"remark,omitempty"`
	NewContainerID    *uint                     `gorm:"index" json:"newContainerId,omitempty"`
	NewContainer      *StorageContainer         `gorm:"foreignKey:NewContainerID" json:"newContainer,omitempty"`
	NewPosition       string                    `gorm:"size:120" json:"newPosition,omitempty"`
	CheckedByID       *uint                     `gorm:"index" json:"checkedById,omitempty"`
	CheckedByName     string                    `gorm:"size:100" json:"checkedByName,omitempty"`
	CheckedAt         *time.Time                `json:"checkedAt,omitempty"`
}

func (i *StocktakeItem) Normalize() {
	i.AccessionNo = strings.ToUpper(strings.TrimSpace(i.AccessionNo))
	i.SampleType = strings.TrimSpace(i.SampleType)
	i.OriginPosition = strings.TrimSpace(i.OriginPosition)
	i.OriginCustodian = strings.TrimSpace(i.OriginCustodian)
	i.Remark = strings.TrimSpace(i.Remark)
	i.NewPosition = strings.TrimSpace(i.NewPosition)
	i.CheckedByName = strings.TrimSpace(i.CheckedByName)
}

func (i StocktakeItem) Validate() error {
	if i.TaskID == 0 || i.SpecimenID == 0 {
		return fmt.Errorf("stocktake item must reference a task and a specimen")
	}
	if i.AccessionNo == "" || i.OriginContainerID == 0 || i.OriginPosition == "" || i.OriginCustodian == "" {
		return fmt.Errorf("stocktake item must snapshot the original container, position and custodian")
	}
	if i.Result != "" && !i.Result.Valid() {
		return fmt.Errorf("unsupported stocktake result: %s", i.Result)
	}
	if len([]rune(i.Remark)) > 1000 || len([]rune(i.NewPosition)) > 120 {
		return fmt.Errorf("stocktake remark or new position is too long")
	}
	if i.Result == "" {
		if i.CheckedByID != nil || i.CheckedByName != "" || i.CheckedAt != nil || i.Remark != "" || i.NewContainerID != nil || i.NewPosition != "" {
			return fmt.Errorf("pending item cannot contain check metadata")
		}
		return nil
	}
	if i.CheckedByID == nil || *i.CheckedByID == 0 || i.CheckedByName == "" || i.CheckedAt == nil {
		return fmt.Errorf("checked item requires checker identity and time")
	}
	switch i.Result {
	case constants.StocktakeResultMissing:
		if i.Remark == "" {
			return fmt.Errorf("missing specimen requires an explanation")
		}
		if i.NewContainerID != nil || i.NewPosition != "" {
			return fmt.Errorf("missing specimen cannot carry a new position")
		}
	case constants.StocktakeResultMismatched:
		if i.NewContainerID == nil || *i.NewContainerID == 0 || i.NewPosition == "" {
			return fmt.Errorf("mismatched specimen requires a new container and position")
		}
		if *i.NewContainerID == i.OriginContainerID && i.NewPosition == i.OriginPosition {
			return fmt.Errorf("the new position must differ from the original position")
		}
	case constants.StocktakeResultInStock:
		if i.NewContainerID != nil || i.NewPosition != "" {
			return fmt.Errorf("in-stock specimen cannot carry a new position")
		}
	}
	return nil
}

func (i StocktakeItem) Pending() bool {
	return i.Result == ""
}
