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

func (s StocktakeState) Finished() bool {
	return s == StocktakeStateClosed || s == StocktakeStateCancelled
}

type StocktakeResult string

const (
	StocktakeResultInStock    StocktakeResult = "in_stock"
	StocktakeResultMissing    StocktakeResult = "missing"
	StocktakeResultMismatched StocktakeResult = "mismatched"
)

func (r StocktakeResult) Valid() bool {
	switch r {
	case StocktakeResultInStock, StocktakeResultMissing, StocktakeResultMismatched:
		return true
	default:
		return false
	}
}

func (r StocktakeResult) Resolved() bool {
	return r == StocktakeResultMissing || r == StocktakeResultMismatched
}
