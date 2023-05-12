package appconsts

import "time"

const (
	TimeoutPropose = time.Second * 10
	// TargetHeightDuration is the intended block interval duration (i.e. the
	// time between blocks). Note that this is a target because CometBFT does
	// not guarantee a fixed block interval.
	TargetHeightDuration = time.Second * 15
)
