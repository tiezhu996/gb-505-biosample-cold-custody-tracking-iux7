package dto

import (
	"biosample-cold-custody-tracking/backend/internal/constants"
)

type CreateStocktakeRequest struct {
	ContainerID uint `json:"containerId" binding:"required"`
}

type MarkStocktakeItemRequest struct {
	Result         constants.StocktakeResult `json:"result" binding:"required,oneof=in_place missing mislocated"`
	Note           string                    `json:"note" binding:"omitempty,max=1000"`
	NewContainerID *uint                     `json:"newContainerId"`
	NewPosition    string                    `json:"newPosition" binding:"omitempty,max=120"`
}

type CloseStocktakeRequest struct {
	Confirm bool `json:"confirm"`
}

type CancelStocktakeRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=1000"`
}
