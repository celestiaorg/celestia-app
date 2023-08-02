package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	NanosecondsPerSecond = 1_000_000_000
	SecondsPerMinute     = 60
	MinutesPerHour       = 60
	HoursPerDay          = 24
	// DaysPerYear is the mean length of the Gregorian calendar year. Note this
	// value isn't 365 because 97 out of 400 years are leap years. See
	// https://en.wikipedia.org/wiki/Year
	DaysPerYear        = 365.2425
	SecondsPerYear     = int64(SecondsPerMinute * MinutesPerHour * HoursPerDay * DaysPerYear) // 31,556,952
	NanosecondsPerYear = int64(NanosecondsPerSecond * SecondsPerYear)                         // 31,556,952,000,000,000

	InitialInflationRate = 0.08
	DisinflationRate     = 0.1
	TargetInflationRate  = 0.015
)

func InitialInflationRateAsDec() sdk.Dec {
	return sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
}

func DisinflationRateAsDec() sdk.Dec {
	return sdk.NewDecWithPrec(DisinflationRate*1000, 3)
}

func TargetInflationRateAsDec() sdk.Dec {
	return sdk.NewDecWithPrec(TargetInflationRate*1000, 3)
}
