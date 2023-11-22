package network

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const (
	// CancelCommand is the name of the command to cancel a command if it is
	// still running. This is a special command because it is not executed
	// normally, instead the Operator executes it directly.
	CancelCommand = "cancel"
	// TestEndName is the name of the command to signal the end of a test. This
	// is a special command because it is not executed normally, instead the
	// Operator executes it directly.
	TestEndName = "test_end"
)

// CommandHandler type defines the signature for command handlers
type CommandHandler func(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, args json.RawMessage) error

func DefaultCommandRegistry() map[string]CommandHandler {
	return make(map[string]CommandHandler)
}

// Command is a struct to represent commands from the leader. Each command has
// an associated handler that describes the execution logic for the command.
type Command struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Args        json.RawMessage `json:"args"`
	Timeout     time.Duration   `json:"timeout"`
	TargetGroup string          `json:"target_group"`
}

// Operator is a struct to manage the execution of commands. This is used to
// orchestrate actions across a network. When a command is received, the
// Operator will spawn a new goroutine to execute the command. The Operator
// will also track the status of each job and cancel any jobs that are still
// running when the Operator is stopped.
type Operator struct {
	groupID  string
	registry map[string]CommandHandler
	mut      *sync.Mutex
	jobs     map[string]context.CancelFunc
	wg       *sync.WaitGroup
}

// NewOperator initialize a new Operator struct.
func NewOperator() *Operator {
	return &Operator{
		registry: DefaultCommandRegistry(),
		jobs:     make(map[string]context.CancelFunc),
		wg:       &sync.WaitGroup{},
		mut:      &sync.Mutex{},
	}
}

// Run starts the Operator and waits for commands to be received that target its group. The Operator
// will spawn a new goroutine for each command received. It will also cancel any
// running jobs when the context is canceled or an error is thrown.
func (o *Operator) Run(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, cmds <-chan Command) error {
	defer o.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-cmds:
			if !ok {
				return nil
			}
			switch cmd.Name {
			case TestEndName:
				runenv.RecordMessage("follower: test ended by leader")
				return nil
			case CancelCommand:
				o.StopJob(cmd.ID)
			default:
				runenv.RecordMessage(fmt.Sprintf("follower: received command %s %+v", cmd.Name, cmd.Args))
				if cmd.TargetGroup != "all" {
					if cmd.TargetGroup != o.groupID {
						continue
					}
				}

				o.mut.Lock()
				if o.jobs[cmd.ID] != nil {
					runenv.RecordMessage(fmt.Sprintf("follower: job with id %s already exists", cmd.ID))
					continue
				}
				o.mut.Unlock()

				handler, exists := o.registry[cmd.Name]
				if !exists {
					runenv.RecordMessage(fmt.Sprintf("follower: job %s with id %s isn't registered", cmd.Name, cmd.ID))
					continue
				}

				runenv.RecordMessage("handler exists")

				tctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
				o.jobs[cmd.ID] = cancel
				o.wg.Add(1)

				go func(ctx context.Context, cmd Command) {
					defer o.wg.Done()
					defer o.StopJob(cmd.ID)
					err := handler(ctx, runenv, initCtx, cmd.Args)
					if err != nil {
						runenv.RecordMessage(fmt.Sprintf("follower: job %s ID %s failed: %s", cmd.Name, cmd.ID, err))
					}
				}(tctx, cmd)

				runenv.RecordMessage("follower: goroutine started")
			}
		}
	}
}

func (o *Operator) StopJob(id string) {
	o.mut.Lock()
	defer o.mut.Unlock()
	if cancel, exists := o.jobs[id]; exists {
		cancel()
		delete(o.jobs, id)
	}
}

func (o *Operator) RegisterCommand(name string, handler CommandHandler) {
	o.mut.Lock()
	defer o.mut.Unlock()
	o.registry[name] = handler
}

// Stop will stop the Operator and wait for all jobs to complete
func (o *Operator) Stop() {
	for id := range o.jobs {
		o.StopJob(id)
	}
	o.wg.Wait()
}

func EndTestCommand() Command {
	return Command{
		ID:          "test_end",
		Name:        TestEndName,
		Timeout:     time.Second * 10,
		TargetGroup: "all",
	}
}
