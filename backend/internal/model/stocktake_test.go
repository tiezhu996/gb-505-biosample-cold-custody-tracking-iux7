package model

import (
	"testing"
	"time"

	"biosample-cold-custody-tracking/backend/internal/constants"
)

func validStocktake() Stocktake {
	now := time.Now().UTC()
	return Stocktake{
		StocktakeNo:   "ST-20260924-001",
		TaskMonth:     "2026-09",
		ContainerID:   3,
		State:         constants.StocktakeStateInProgress,
		TotalItems:    3,
		StartedByID:   2,
		StartedByName: "冻存保管员",
		StartedAt:     now,
	}
}

func TestOpenStocktakeValidation(t *testing.T) {
	task := validStocktake()
	if err := task.Validate(); err != nil {
		t.Fatalf("valid open stocktake rejected: %v", err)
	}
	if task.ProgressPercent() != 0 || task.AllProcessed() {
		t.Fatal("new stocktake must show zero progress")
	}
	task.ProcessedItems, task.InPlaceItems = 3, 3
	if err := task.Validate(); err != nil {
		t.Fatalf("fully processed stocktake rejected: %v", err)
	}
	if !task.AllProcessed() || task.ProgressPercent() != 100 {
		t.Fatal("processed counters must report completion")
	}
}

func TestStocktakeCounterConsistency(t *testing.T) {
	task := validStocktake()
	task.ProcessedItems, task.InPlaceItems, task.MissingItems, task.MislocatedItems = 3, 1, 1, 1
	if err := task.Validate(); err != nil {
		t.Fatalf("consistent counters rejected: %v", err)
	}
	task.ProcessedItems = 2
	if err := task.Validate(); err == nil {
		t.Fatal("processed count must equal the sum of result counters")
	}
}

func TestClosedStocktakeRequiresCloser(t *testing.T) {
	now := time.Now().UTC()
	closerID := uint(2)
	task := validStocktake()
	task.TotalItems, task.ProcessedItems, task.InPlaceItems = 1, 1, 1
	task.State = constants.StocktakeStateClosed
	task.ClosedByID, task.ClosedByName, task.ClosedAt = &closerID, "冻存保管员", &now
	if err := task.Validate(); err != nil {
		t.Fatalf("valid closed stocktake rejected: %v", err)
	}
}

func TestStocktakeItemSnapshotAndMarking(t *testing.T) {
	now := time.Now().UTC()
	markerID := uint(2)
	item := StocktakeItem{
		StocktakeID:         9,
		SpecimenID:          11,
		OriginContainerID:   3,
		OriginContainerCode: "FZ-80-B02",
		OriginContainerName: "负八十度二号冻存柜",
		OriginPosition:      "R02-BX04-A03",
		OriginLocation:      "样本库 B 区 / FZ-80-B02 / R02-BX04-A03",
		Result:              constants.StocktakeResultPending,
	}
	if err := item.Validate(); err != nil {
		t.Fatalf("pending snapshot rejected: %v", err)
	}
	item.Result = constants.StocktakeResultMissing
	item.Note = "管身无标识，按缺失登记"
	item.MarkedByID, item.MarkedByName, item.MarkedAt = &markerID, "冻存保管员", &now
	if err := item.Validate(); err != nil {
		t.Fatalf("missing item with note rejected: %v", err)
	}
	item.Result = constants.StocktakeResultMislocated
	item.Note = ""
	item.NewContainerID = &markerID
	item.NewContainerCode = "FZ-20-A01"
	item.NewPosition = "S09-BX01-A01"
	if err := item.Validate(); err != nil {
		t.Fatalf("mislocated item with new location rejected: %v", err)
	}
}

func TestStocktakeItemMissingRequiresNote(t *testing.T) {
	now := time.Now().UTC()
	markerID := uint(2)
	item := StocktakeItem{
		StocktakeID: 1, SpecimenID: 1, OriginContainerID: 3,
		OriginContainerCode: "FZ-80-B02", OriginPosition: "R01",
		Result: constants.StocktakeResultMissing, MarkedByID: &markerID,
		MarkedByName: "保管员", MarkedAt: &now,
	}
	if err := item.Validate(); err == nil {
		t.Fatal("missing item without a note must be rejected")
	}
}

func TestStocktakeItemMislocatedRequiresTarget(t *testing.T) {
	now := time.Now().UTC()
	markerID := uint(2)
	item := StocktakeItem{
		StocktakeID: 1, SpecimenID: 1, OriginContainerID: 3,
		OriginContainerCode: "FZ-80-B02", OriginPosition: "R01",
		Result: constants.StocktakeResultMislocated, MarkedByID: &markerID,
		MarkedByName: "保管员", MarkedAt: &now,
	}
	if err := item.Validate(); err == nil {
		t.Fatal("mislocated item without a new container and position must be rejected")
	}
}
