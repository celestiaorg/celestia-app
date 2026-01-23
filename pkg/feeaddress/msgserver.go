package feeaddress

import (
	"context"
)

// msgServerImpl implements the feeaddress MsgServer interface.
// The actual fee forwarding is done by ProtocolFeeTerminatorDecorator in the ante handler.
// This handler exists only to satisfy the message router - it's a no-op since
// all work is done in the ante handler before this is called.
type msgServerImpl struct{}

var _ MsgServer = (*msgServerImpl)(nil)

// NewMsgServerImpl creates a new MsgServer implementation.
func NewMsgServerImpl() MsgServer {
	return &msgServerImpl{}
}

// PayProtocolFee handles MsgPayProtocolFee. This is a no-op handler because:
// 1. ProtocolFeeTerminatorDecorator in the ante handler already forwarded the fees
// 2. This handler is only called to complete the message execution flow
// 3. The fee forwarding has already happened before this point
func (m msgServerImpl) PayProtocolFee(_ context.Context, _ *MsgPayProtocolFee) (*MsgPayProtocolFeeResponse, error) {
	return &MsgPayProtocolFeeResponse{}, nil
}
