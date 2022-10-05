package orchestrator

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/app"
	qgbtypes "github.com/celestiaorg/celestia-app/x/qgb/types"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdkcodectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/gogo/protobuf/proto"
	coretypes "github.com/tendermint/tendermint/types"
)

var _ QGBParserI = QGBParser{}

type QGBParserI interface {
	IsDataCommitmentConfirm(msg sdktypes.Msg) (bool, error)
	ParseDataCommitmentConfirm(msg sdktypes.Msg) (qgbtypes.MsgDataCommitmentConfirm, error)
	IsValsetConfirm(msg sdktypes.Msg) (bool, error)
	ParseValsetConfirm(msg sdktypes.Msg) (qgbtypes.MsgValsetConfirm, error)
	ParseCoreTx(tx coretypes.Tx) (sdktx.Tx, error)
	ParseSdkTx(any *sdkcodectypes.Any) (sdktypes.Msg, error)
}

type QGBParser struct {
	codec sdkcodec.Codec
}

func NewQGBParser(codec sdkcodec.Codec) *QGBParser {
	return &QGBParser{codec: codec}
}

func (parser QGBParser) IsValsetConfirm(msg sdktypes.Msg) (bool, error) {
	switch msg.(type) {
	case *qgbtypes.MsgValsetConfirm:
		return true, nil
	default:
		return false, nil
	}
}

func (parser QGBParser) IsDataCommitmentConfirm(msg sdktypes.Msg) (bool, error) {
	switch msg.(type) {
	case *qgbtypes.MsgDataCommitmentConfirm:
		return true, nil
	default:
		return false, nil
	}
}

func (parser QGBParser) ParseValsetConfirm(msg sdktypes.Msg) (qgbtypes.MsgValsetConfirm, error) {
	vs, ok := msg.(*qgbtypes.MsgValsetConfirm)
	if !ok {
		return qgbtypes.MsgValsetConfirm{}, fmt.Errorf("not good")
	}
	return *vs, nil
}

func (parser QGBParser) ParseDataCommitmentConfirm(msg sdktypes.Msg) (qgbtypes.MsgDataCommitmentConfirm, error) {
	dcc, ok := msg.(*qgbtypes.MsgDataCommitmentConfirm)
	if !ok {
		return qgbtypes.MsgDataCommitmentConfirm{}, fmt.Errorf("not good")
	}
	return *dcc, nil
}

func (parser QGBParser) ParseCoreTx(tx coretypes.Tx) (sdktx.Tx, error) {
	var sdkTx sdktx.Tx
	err := proto.Unmarshal(tx, &sdkTx)
	if err != nil {
		return sdktx.Tx{}, fmt.Errorf("error while unmarshalling core transaction: %s", err)
	}
	return sdkTx, nil
}

func (parser QGBParser) ParseSdkTx(any *sdkcodectypes.Any) (sdktypes.Msg, error) {
	var stdMsg sdktypes.Msg
	err := parser.codec.UnpackAny(any, &stdMsg)
	if err != nil {
		return nil, fmt.Errorf("error while unpacking message: %s", err)
	}
	return stdMsg, nil
}

func MakeDefaultAppCodec() sdkcodec.Codec {
	interfaceRegistry := sdkcodectypes.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	app.ModuleBasics.RegisterInterfaces(interfaceRegistry)
	qgbtypes.RegisterInterfaces(interfaceRegistry)
	return sdkcodec.NewProtoCodec(interfaceRegistry)
}
