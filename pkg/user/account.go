package user

import (
	"context"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/grpc"
)

type Account struct {
	name          string
	address       types.AccAddress
	pubKey        cryptotypes.PubKey
	accountNumber uint64

	// the signers local view of the sequence number
	sequence uint64
}

func NewAccount(keyName string, accountNumber, sequenceNumber uint64) *Account {
	return &Account{
		name:          keyName,
		accountNumber: accountNumber,
		sequence:      sequenceNumber,
	}
}

func (a Account) Name() string {
	return a.name
}

func (a Account) Address() types.AccAddress {
	return a.address
}

func (a Account) PubKey() cryptotypes.PubKey {
	return a.pubKey
}

func (a Account) AccountNumber() uint64 {
	return a.accountNumber
}

// Sequence returns the sequence number of the account.
// This is locally tracked
func (a Account) Sequence() uint64 {
	return a.sequence
}

func (a *Account) Copy() *Account {
	return &Account{
		name:          a.name,
		address:       a.address,
		pubKey:        a.pubKey,
		accountNumber: a.accountNumber,
		sequence:      a.sequence,
	}
}

// QueryAccount fetches the account number and sequence number from the celestia-app node.
func QueryAccount(ctx context.Context, conn *grpc.ClientConn, registry codectypes.InterfaceRegistry, address types.AccAddress) (accNum uint64, seqNum uint64, err error) {
	qclient := authtypes.NewQueryClient(conn)
	// TODO: ideally we add a way to prove that the accounts rather than simply trusting the full node we are connected with
	resp, err := qclient.Account(
		ctx,
		&authtypes.QueryAccountRequest{Address: address.String()},
	)
	if err != nil {
		return accNum, seqNum, err
	}

	var acc types.AccountI
	err = registry.UnpackAny(resp.Account, &acc)
	if err != nil {
		return accNum, seqNum, err
	}

	accNum, seqNum = acc.GetAccountNumber(), acc.GetSequence()
	return accNum, seqNum, nil
}
