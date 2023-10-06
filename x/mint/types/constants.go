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

	// InitialInflationRate is the inflation rate that the network starts at.
	InitialInflationRate = 0.08
	// DisinflationRate is the rate at which the inflation rate decreases each year.
	DisinflationRate = 0.1
	// TargetInflationRate is the inflation rate that the network aims to
	// stabalize at. In practice, TargetInflationRate acts as a minimum so that
	// the inflation rate doesn't decrease after reaching it.
	TargetInflationRate = 0.015
)

var (
	initialInflationRateAsDec = sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
	disinflationRateAsDec     = sdk.NewDecWithPrec(DisinflationRate*1000, 3)
	targetInflationRateAsDec  = sdk.NewDecWithPrec(TargetInflationRate*1000, 3)
)

func InitialInflationRateAsDec() sdk.Dec {
	return initialInflationRateAsDec
}

func DisinflationRateAsDec() sdk.Dec {
	return disinflationRateAsDec
}

func TargetInflationRateAsDec() sdk.Dec {
	return targetInflationRateAsDec
}
