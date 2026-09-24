package constants

import "testing"

func TestStocktakeStates(t *testing.T) {
	if !StocktakeStateInProgress.Open() || StocktakeStateClosed.Open() || StocktakeStateCancelled.Open() {
		t.Fatal("only in_progress stocktakes are open")
	}
	for _, state := range []StocktakeState{StocktakeStateInProgress, StocktakeStateClosed, StocktakeStateCancelled} {
		if !state.Valid() {
			t.Fatalf("stocktake state %s must be valid", state)
		}
	}
	if StocktakeState("finished").Valid() {
		t.Fatal("unknown stocktake state must be invalid")
	}
	if !StocktakeStateInProgress.CanClose() || StocktakeStateClosed.CanClose() {
		t.Fatal("only open stocktakes can be closed")
	}
}

func TestStocktakeResults(t *testing.T) {
	if StocktakeResultPending.Resolved() {
		t.Fatal("pending items are not resolved")
	}
	for _, result := range []StocktakeResult{StocktakeResultInPlace, StocktakeResultMissing, StocktakeResultMislocated} {
		if !result.Valid() || !result.Resolved() {
			t.Fatalf("result %s must be a valid resolved outcome", result)
		}
	}
	if StocktakeResult("lost").Valid() {
		t.Fatal("unknown stocktake result must be invalid")
	}
}

func TestCustodianCanManageStocktakes(t *testing.T) {
	if !RoleCustodian.Can("stocktake:prepare") || !RoleCustodian.Can("stocktake:close") {
		t.Fatal("custodian must prepare and close stocktakes")
	}
	if !RoleAdmin.Can("stocktake:close") {
		t.Fatal("administrator must have every permission")
	}
	if RoleReceiver.Can("stocktake:prepare") || RoleAuditor.Can("stocktake:close") {
		t.Fatal("receivers and auditors must not manage stocktakes")
	}
}
