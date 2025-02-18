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
	NanosecondsPerYear = NanosecondsPerSecond * SecondsPerYear                                // 31,556,952,000,000,000

	// InitialInflationRate is the inflation rate that the network starts at.
	InitialInflationRate = 0.08
	// DisinflationRate is the rate at which the inflation rate decreases each year.
	DisinflationRate = 0.1
	// InitialInflationRateCip29 is the inflation rate specified in CIP-29.
	InitialInflationRateCip29 = 0.0536
	// DisinflationRateCip29 is the rate at which the inflation rate decreases each year (after CIP-29 was introduced).
	DisinflationRateCip29 = 0.067
	// TargetInflationRate is the inflation rate that the network aims to
	// stabilize at. In practice, TargetInflationRate acts as a minimum so that
	// the inflation rate doesn't decrease after reaching it.
	TargetInflationRate = 0.015
)

var (
	initialInflationRateAsDec      = sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
	initialInflationRateCip29AsDec = sdk.NewDecWithPrec(InitialInflationRateCip29*10000, 4)
	disinflationRateAsDec          = sdk.NewDecWithPrec(DisinflationRate*1000, 3)
	disinflationRateCip29AsDec     = sdk.NewDecWithPrec(DisinflationRateCip29*1000, 3)
	targetInflationRateAsDec       = sdk.NewDecWithPrec(TargetInflationRate*1000, 3)
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

func InitialInflationRateCip29AsDec() sdk.Dec {
	return initialInflationRateCip29AsDec
}

func DisinflationRateCip29AsDec() sdk.Dec {
	return disinflationRateCip29AsDec
}
