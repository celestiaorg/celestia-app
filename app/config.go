package app

func init() {
	// init() was invoked by celestia-app v1.x so we don't need to perform any
	// additional config set up it in celestia-app v2.x. In fact, config can't
	// be modified by v2.x because we'll hit a panic: "config is sealed".
}
