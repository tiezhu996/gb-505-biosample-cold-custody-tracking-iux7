package dto

import "biosample-cold-custody-tracking/backend/internal/constants"

type CreateStocktakeRequest struct {
	TaskNo      string `json:"taskNo" binding:"required,min=3,max=50"`
	ContainerID uint   `json:"containerId" binding:"required"`
	Note        string `json:"note" binding:"omitempty,max=1000"`
}

type MarkStocktakeItemRequest struct {
	Result         constants.StocktakeResult `json:"result" binding:"required,oneof=in_stock missing mismatched"`
	Remark         string                    `json:"remark" binding:"omitempty,max=1000"`
	NewContainerID *uint                     `json:"newContainerId"`
	NewPosition    *string                   `json:"newPosition" binding:"omitempty,max=120"`
}

type CancelStocktakeRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=1000"`
}
