package testnode

import (
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
)

func NewOfflineSigner() (*user.Signer, error) {
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	kr, _ := NewKeyring(testfactory.TestAccName)
	return user.NewSigner(kr, encCfg.TxConfig, testfactory.ChainID, user.NewAccount(testfactory.TestAccName, 0, 0))
}

func NewTxClientFromContext(ctx Context) (*user.TxClient, error) {
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	return user.SetupTxClient(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg)
}
