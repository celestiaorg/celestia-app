package app

import (
	"time"

	"github.com/celestiaorg/celestia-app/v2/app/ante"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/da"
	"github.com/celestiaorg/go-square/blob"
	"github.com/celestiaorg/go-square/shares"
	"github.com/celestiaorg/go-square/square"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	icatypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/types"
	ibctypes "github.com/cosmos/ibc-go/v6/modules/core/04-channel/types"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	version "github.com/tendermint/tendermint/proto/tendermint/version"
)

// PrepareProposal fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. This method generates the data root for
// the proposal block and passes it back to tendermint via the BlockData. Panics
// indicate a developer error and should immediately halt the node for
// visibility and so they can be quickly resolved.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	defer telemetry.MeasureSince(time.Now(), "prepare_proposal")
	// Create a context using a branch of the state.
	sdkCtx := app.NewProposalContext(core.Header{
		ChainID: req.ChainId,
		Height:  req.Height,
		Time:    req.Time,
		Version: version.Consensus{
			App: app.BaseApp.AppVersion(),
		},
	})
	handler := ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		app.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.ParamsKeeper,
		app.MsgGateKeeper,
	)

	// Filter out invalid transactions.
	txs := FilterTxs(app.Logger(), sdkCtx, handler, app.txConfig, req.BlockData.Txs)
	txs = filterICATxs(app, app.txConfig, txs)

	// Build the square from the set of valid and prioritised transactions.
	// The txs returned are the ones used in the square and block.
	dataSquare, txs, err := square.Build(txs,
		app.MaxEffectiveSquareSize(sdkCtx),
		appconsts.SubtreeRootThreshold(app.GetBaseApp().AppVersion()),
	)
	if err != nil {
		panic(err)
	}

	// Erasure encode the data square to create the extended data square (eds).
	// Note: uses the nmt wrapper to construct the tree. See
	// pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		app.Logger().Error(
			"failure to create new data availability header",
			"error",
			err.Error(),
		)
		panic(err)
	}

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return abci.ResponsePrepareProposal{
		BlockData: &core.Data{
			Txs:        txs,
			SquareSize: uint64(dataSquare.Size()),
			Hash:       dah.Hash(), // also known as the data root
		},
	}
}

// filterICATxs filters out ICA txs that include a message that is not on icaAllowedMessages.
// This is needed because the ICA host module AllowMessages param != icaAllowMessages().
// TODO: This block can be removed after the ICA host param AllowMessages == icaAllowMessages().
func filterICATxs(app *App, txConfig client.TxConfig, txs [][]byte) (result [][]byte) {
	for _, tx := range txs {
		_, isBlob := blob.UnmarshalBlobTx(tx)
		if isBlob {
			result = append(result, tx)
			continue
		}
		sdkTx, err := txConfig.TxDecoder()(tx)
		if err != nil {
			result = append(result, tx)
			continue
		}
		msgs := sdkTx.GetMsgs()
		for _, msg := range msgs {
			if recvPacketMsg, ok := msg.(*ibctypes.MsgRecvPacket); ok {
				var data icatypes.InterchainAccountPacketData
				if err := icatypes.ModuleCdc.UnmarshalJSON(recvPacketMsg.Packet.GetData(), &data); err != nil {
					// Let ICA host module return an error for this.
					result = append(result, tx)
					continue
				}
				if data.Type != icatypes.EXECUTE_TX {
					// No action needed if this is not an EXECUTE_TX.
					result = append(result, tx)
					continue
				}
				icaMsgs, err := icatypes.DeserializeCosmosTx(app.AppCodec(), data.Data)
				if err != nil {
					// Let ICA host module return an error code for this.
					result = append(result, tx)
					continue
				}
				for _, icaMsg := range icaMsgs {
					if isAllowed := icahosttypes.ContainsMsgType(icaAllowMessages(), icaMsg); isAllowed {
						result = append(result, tx)
						continue
					}
					app.Logger().Debug("ICA message is not allowed", "msg", icaMsg)
				}
			} else {
				result = append(result, tx)
			}
		}
	}
	return result
}
