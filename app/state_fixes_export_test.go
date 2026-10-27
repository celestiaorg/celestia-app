package app

// SetStateFixes replaces the state fixes of this app instance. Tests only.
func (app *App) SetStateFixes(fixes []StateFix) {
	app.stateFixes = fixes
}

// StateFixes returns the state fixes this release ships.
func StateFixes() []StateFix {
	return stateFixes
}
