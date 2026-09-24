package model

import (
	"testing"
	"time"

	"biosample-cold-custody-tracking/backend/internal/constants"
)

func validStocktakeTask() StocktakeTask {
	return StocktakeTask{
		TaskNo:        "PD-20260924-001",
		ContainerID:   2,
		State:         constants.StocktakeStateInProgress,
		TotalCount:    3,
		PendingCount:  3,
		StartedByID:   5,
		StartedByName: "冻存保管员",
		StartedAt:     time.Now().Add(-time.Hour),
	}
}

func TestStocktakeCounterInvariant(t *testing.T) {
	task := validStocktakeTask()
	if err := task.Validate(); err != nil {
		t.Fatalf("valid in-progress stocktake rejected: %v", err)
	}
	task.PendingCount, task.InStockCount = 2, 2
	if err := task.Validate(); err == nil {
		t.Fatal("counters that do not add up to total must fail validation")
	}
	now := time.Now()
	closerID := uint(5)
	task.PendingCount, task.InStockCount, task.MissingCount = 0, 2, 1
	task.State = constants.StocktakeStateClosed
	task.ClosedByID = &closerID
	task.ClosedByName = "冻存保管员"
	task.ClosedAt = &now
	if err := task.Validate(); err != nil {
		t.Fatalf("valid closed stocktake rejected: %v", err)
	}
	if task.AllResolved() {
		t.Fatal("closed task is no longer eligible for closure checks")
	}
}

func TestStocktakeItemMarkingRules(t *testing.T) {
	checkerID := uint(5)
	now := time.Now()
	originContainer := uint(2)
	newContainer := uint(3)
	base := StocktakeItem{
		TaskID: 1, SpecimenID: 11, AccessionNo: "BIO-20260819-001", SampleType: "血浆",
		OriginContainerID: originContainer, OriginPosition: "R02-BX04-A03", OriginCustodian: "冻存保管员",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("fresh snapshot item rejected: %v", err)
	}

	missing := base
	missing.Result = constants.StocktakeResultMissing
	missing.CheckedByID = &checkerID
	missing.CheckedByName = "冻存保管员"
	missing.CheckedAt = &now
	if err := missing.Validate(); err == nil {
		t.Fatal("missing item without explanation must fail validation")
	}
	missing.Remark = "架位空缺，疑似取出未登记"
	if err := missing.Validate(); err != nil {
		t.Fatalf("explained missing item rejected: %v", err)
	}

	mismatched := base
	mismatched.Result = constants.StocktakeResultMismatched
	mismatched.CheckedByID = &checkerID
	mismatched.CheckedByName = "冻存保管员"
	mismatched.CheckedAt = &now
	if err := mismatched.Validate(); err == nil {
		t.Fatal("mismatched item without a new position must fail validation")
	}
	mismatched.NewContainerID = &originContainer
	mismatched.NewPosition = mismatched.OriginPosition
	if err := mismatched.Validate(); err == nil {
		t.Fatal("new position identical to the original must fail validation")
	}
	mismatched.NewContainerID = &newContainer
	mismatched.NewPosition = "R01-BX01-A01"
	if err := mismatched.Validate(); err != nil {
		t.Fatalf("relocated mismatched item rejected: %v", err)
	}

	inStock := base
	inStock.Result = constants.StocktakeResultInStock
	inStock.CheckedByID = &checkerID
	inStock.CheckedByName = "冻存保管员"
	inStock.CheckedAt = &now
	if err := inStock.Validate(); err != nil {
		t.Fatalf("in-stock item rejected: %v", err)
	}
}
