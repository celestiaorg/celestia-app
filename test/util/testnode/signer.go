package testnode

import (
	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
)

func NewOfflineSigner() (*user.Signer, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, addr := NewKeyring(testfactory.TestAccName)
	return user.NewSigner(kr, nil, addr[0], encCfg.TxConfig, testfactory.ChainID, 1, 0, appconsts.LatestVersion)
}

func NewSingleSignerFromContext(ctx Context) (*user.Signer, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	return user.SetupSingleSigner(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, encCfg)
}

func NewSignerFromContext(ctx Context, acc string) (*user.Signer, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	addr := testfactory.GetAddress(ctx.Keyring, acc)
	return user.SetupSigner(ctx.GoContext(), ctx.Keyring, ctx.GRPCClient, addr, encCfg)
}
