package orchestrator

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sync"
)

var _ Engine = &orchestratorEngine{}

type orchestratorEngine struct {
	context      context.Context
	orch         orchestrator
	timeout      uint64
	replayOld    bool
	followLatest bool
}

func NewOrchestratorEngine(ctx context.Context, orch orchestrator, timeout uint64, replay bool, follow bool) *orchestratorEngine {
	return &orchestratorEngine{
		context:      ctx,
		orch:         orch,
		timeout:      timeout,
		replayOld:    replay,
		followLatest: follow,
	}
}

func (engine orchestratorEngine) Start() error {
	var err error
	wg := &sync.WaitGroup{}
	if engine.replayOld {
		// replay logic
		wg.Add(1)
		go func() {
			select {
			case <-engine.context.Done():
				return
			default:
				err = engine.replay()
				return
			}
		}()
	}
	if engine.followLatest {
		wg.Add(1)
		go func() {
			select {
			case <-engine.context.Done():
				return
			default:
				err = engine.follow()
				return
			}
		}()
	}
	wg.Wait()
	return err
}

func (engine orchestratorEngine) orchestrateValsets() error {
	valsetChan, err := engine.orch.appClient.SubscribeValset(engine.context)
	if err != nil {
		retrier := valsetSubscriptionRetrier{
			logger:    engine.orch.logger,
			ctx:       engine.context,
			output:    &valsetChan,
			appClient: engine.orch.appClient,
		}
		err = retrier.retry()
		if err != nil {
			return err
		}
	}
	var msgsChan <-chan sdk.Msg
	msgsChan, err = engine.orch.processValsetEvents(engine.context, valsetChan)
	if err != nil {
		// should we retry here?
		return err
	}
	err = engine.broadcastTxs(msgsChan)
	if err != nil {
		// should we retry here?
		return err
	}
	return err
}

func (engine orchestratorEngine) orchestrateDataCommitments() error {
	return nil
}

func (engine orchestratorEngine) broadcastTxs(msgs <-chan sdk.Msg) error {
	var err error = nil
	go func() {
		for {
			select {
			case <-engine.context.Done():
				return
			case msg := <-msgs:
				err = engine.orch.appClient.BroadcastTx(engine.context, msg)
				if err != nil {
					retrier := msgRetrier{
						logger:    engine.orch.logger,
						ctx:       engine.context,
						appClient: engine.orch.appClient,
						msg:       msg,
					}
					err = retrier.retry()
					if err != nil {
						return
					}
				}
			}
		}
	}()

	return err
}

func (engine orchestratorEngine) Stop() error {
	return nil
}

func (engine orchestratorEngine) replay() error {
	return nil
}

func (engine orchestratorEngine) follow() error {
	var err error = nil
	ctx, cancel := context.WithCancel(engine.context)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
			err = engine.orchestrateValsets()
			if err != nil {
				cancel()
				return
			}
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		default:
			err = engine.orchestrateDataCommitments()
			if err != nil {
				cancel()
				return
			}
			cancel()
		}
	}()

	wg.Wait()
	return err
}
