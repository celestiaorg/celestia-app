package types

import (
	"cosmossdk.io/math"
)

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

	// InitialInflationRate is the inflation rate as defined in CIP-29.
	InitialInflationRate = 0.0536
	// DisinflationRate is the rate at which the inflation rate decreases each year as defined in CIP-29.
	DisinflationRate = 0.067
	// TargetInflationRate is the inflation rate that the network aims to
	// stabilize at. In practice, TargetInflationRate acts as a minimum so that
	// the inflation rate doesn't decrease after reaching it.
	TargetInflationRate = 0.015
)

var (
	initialInflationRateAsDec = math.LegacyNewDecWithPrec(InitialInflationRate*10000, 4)
	disinflationRateAsDec     = math.LegacyNewDecWithPrec(DisinflationRate*1000, 3)
	targetInflationRateAsDec  = math.LegacyNewDecWithPrec(TargetInflationRate*1000, 3)
)

func InitialInflationRateAsDec() math.LegacyDec {
	return initialInflationRateAsDec
}

func DisinflationRateAsDec() math.LegacyDec {
	return disinflationRateAsDec
}

func TargetInflationRateAsDec() math.LegacyDec {
	return targetInflationRateAsDec
}
