package app

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func QueryAccount(addr string) error {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	conn, err := grpc.Dial("consensus.lunaroasis.net:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}

	authQC := authtypes.NewQueryClient(conn)

	authresp, err := authQC.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: addr,
	})
	if err != nil {
		return err
	}

	var acc authtypes.AccountI
	err = ecfg.InterfaceRegistry.UnpackAny(authresp.Account, &acc)
	if err != nil {
		return err
	}

	switch acc := acc.(type) {
	case *vestingtypes.PeriodicVestingAccount:
		fmt.Println("periodic", acc)
	case *vestingtypes.ContinuousVestingAccount:
		fmt.Println("continuous", acc)
	case *authtypes.BaseAccount:
		fmt.Println("base account", acc)
	default:
		fmt.Println("unknown account type", acc)
	}

	bankQC := banktypes.NewQueryClient(conn)
	spendableResp, err := bankQC.SpendableBalances(context.Background(), &banktypes.QuerySpendableBalancesRequest{
		Address: addr,
	})
	if err != nil {
		return err
	}
	fmt.Println(spendableResp.Balances)
	return nil
}
