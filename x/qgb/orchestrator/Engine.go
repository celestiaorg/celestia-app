package orchestrator

type Engine interface {
	Start() error
	Stop() error
	replay() error
	follow() error
}
