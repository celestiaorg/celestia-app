package orchestrator

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

// TODO use maybe https://github.com/cenkalti/backoff

type msgRetrier struct {
	logger    tmlog.Logger
	msg       sdk.Msg
	ctx       context.Context
	appClient AppClient
}

func (mr msgRetrier) retry() error {
	var err error
	for i := 0; i < 10; i++ {
		err = mr.appClient.BroadcastTx(mr.ctx, mr.msg)
		if err != nil {
			mr.logger.Error(err.Error())
			i++
		} else {
			return nil
		}
	}
	return err
}

type valsetSubscriptionRetrier struct {
	logger    tmlog.Logger
	ctx       context.Context
	output    *<-chan types.Valset
	appClient AppClient
}

func (vsr *valsetSubscriptionRetrier) retry() error {
	var err error
	var out <-chan types.Valset
	for i := 0; i < 10; i++ {
		out, err = vsr.appClient.SubscribeValset(vsr.ctx)
		if err != nil {
			vsr.logger.Error(err.Error())
			i++
		} else {
			vsr.output = &out
			return nil
		}
	}
	return err
}
