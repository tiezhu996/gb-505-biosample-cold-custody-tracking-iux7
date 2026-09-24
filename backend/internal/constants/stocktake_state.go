package constants

type StocktakeState string

const (
	StocktakeStateInProgress StocktakeState = "in_progress"
	StocktakeStateClosed     StocktakeState = "closed"
	StocktakeStateCancelled  StocktakeState = "cancelled"
)

func (s StocktakeState) Valid() bool {
	switch s {
	case StocktakeStateInProgress, StocktakeStateClosed, StocktakeStateCancelled:
		return true
	default:
		return false
	}
}

func (s StocktakeState) Open() bool {
	return s == StocktakeStateInProgress
}

func (s StocktakeState) CanCancel() bool {
	return s == StocktakeStateInProgress
}

func (s StocktakeState) CanClose() bool {
	return s == StocktakeStateInProgress
}

type StocktakeResult string

const (
	StocktakeResultPending    StocktakeResult = "pending"
	StocktakeResultInPlace    StocktakeResult = "in_place"
	StocktakeResultMissing    StocktakeResult = "missing"
	StocktakeResultMislocated StocktakeResult = "mislocated"
)

func (r StocktakeResult) Valid() bool {
	switch r {
	case StocktakeResultPending, StocktakeResultInPlace, StocktakeResultMissing, StocktakeResultMislocated:
		return true
	default:
		return false
	}
}

func (r StocktakeResult) Resolved() bool {
	return r != StocktakeResultPending
}

func StocktakeResults() []StocktakeResult {
	return []StocktakeResult{StocktakeResultInPlace, StocktakeResultMissing, StocktakeResultMislocated}
}
