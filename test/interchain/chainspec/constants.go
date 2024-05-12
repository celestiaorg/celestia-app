package chainspec

func numValidators() *int {
	nodes := 1
	return &nodes
}

func numFullNodes() *int {
	nodes := 0
	return &nodes
}

func gasAdjustment() *float64 {
	gasAdjustment := 2.0
	return &gasAdjustment
}
