package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"biosample-cold-custody-tracking/backend/internal/dto"
	"biosample-cold-custody-tracking/backend/internal/repository"
	"biosample-cold-custody-tracking/backend/internal/service"
	"biosample-cold-custody-tracking/backend/internal/util"
)

type StocktakeHandler struct{ service service.StocktakeService }

func NewStocktakeHandler(stocktakeService service.StocktakeService) *StocktakeHandler {
	return &StocktakeHandler{service: stocktakeService}
}

func (h *StocktakeHandler) List(c *gin.Context) {
	var filter repository.StocktakeFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		util.RespondError(c, util.BadRequest(err.Error()))
		return
	}
	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, result)
}

func (h *StocktakeHandler) Get(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, item)
}

func (h *StocktakeHandler) ListItems(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	var filter repository.StocktakeItemFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		util.RespondError(c, util.BadRequest(err.Error()))
		return
	}
	result, err := h.service.ListItems(c.Request.Context(), id, filter)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, result)
}

func (h *StocktakeHandler) Create(c *gin.Context) {
	var input dto.CreateStocktakeRequest
	if !util.BindJSON(c, &input) {
		return
	}
	item, err := h.service.Create(c.Request.Context(), ActorFromContext(c), input)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusCreated, item)
}

func (h *StocktakeHandler) MarkItem(c *gin.Context) {
	itemID, ok := util.ParseID(c)
	if !ok {
		return
	}
	var input dto.MarkStocktakeItemRequest
	if !util.BindJSON(c, &input) {
		return
	}
	item, err := h.service.MarkItem(c.Request.Context(), ActorFromContext(c), itemID, input)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, item)
}

func (h *StocktakeHandler) Close(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	item, err := h.service.Close(c.Request.Context(), ActorFromContext(c), id)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, item)
}

func (h *StocktakeHandler) Cancel(c *gin.Context) {
	id, ok := util.ParseID(c)
	if !ok {
		return
	}
	var input dto.CancelStocktakeRequest
	if !util.BindJSON(c, &input) {
		return
	}
	item, err := h.service.Cancel(c.Request.Context(), ActorFromContext(c), id, input)
	if err != nil {
		util.RespondError(c, err)
		return
	}
	util.Respond(c, http.StatusOK, item)
}
