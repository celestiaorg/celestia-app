package app

import (
	"bytes"

	"cosmossdk.io/collections"
	collectionscodec "cosmossdk.io/collections/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// proposalAccountKeeper reuses decoding of the last base account during the
// ordered ante walk. The collection still performs every store read and write.
func (app *App) proposalAccountKeeper() authkeeper.AccountKeeper {
	keeper := app.AccountKeeper
	builder := collections.NewSchemaBuilder(runtime.NewKVStoreService(app.keys[authtypes.StoreKey]))
	keeper.Accounts = collections.NewIndexedMap(builder, authtypes.AddressStoreKeyPrefix, "accounts",
		keeper.Accounts.KeyCodec(), &proposalAccountCodec{ValueCodec: keeper.Accounts.ValueCodec()}, keeper.Accounts.Indexes)
	return keeper
}

// maxRememberedAccountSize bounds the bytes the codec keeps a copy of.
const maxRememberedAccountSize = 4096

type proposalAccountCodec struct {
	collectionscodec.ValueCodec[sdk.AccountI]
	// raw is the encoding of account; empty means nothing is remembered.
	raw         []byte
	account     authtypes.BaseAccount
	addressText string
	address     sdk.AccAddress
}

func (c *proposalAccountCodec) Decode(raw []byte) (sdk.AccountI, error) {
	if len(c.raw) > 0 && bytes.Equal(raw, c.raw) {
		// Ante changes sequence and replaces the PubKey field; it does not
		// mutate the public key itself. Give each caller its own account.
		return &proposalBaseAccount{BaseAccount: c.account, codec: c}, nil
	}
	account, err := c.ValueCodec.Decode(raw)
	if err != nil {
		return nil, err
	}
	c.remember(raw, account)
	if base, ok := account.(*authtypes.BaseAccount); ok && len(c.raw) > 0 {
		account = &proposalBaseAccount{BaseAccount: *base, codec: c}
	}
	return account, nil
}

// Encode primes the same byte-checked cache for the account just written.
func (c *proposalAccountCodec) Encode(account sdk.AccountI) ([]byte, error) {
	if wrapped, ok := account.(*proposalBaseAccount); ok {
		account = &wrapped.BaseAccount
	}
	raw, err := c.ValueCodec.Encode(account)
	if err != nil {
		return nil, err
	}
	c.remember(raw, account)
	return raw, nil
}

// remember keeps a copy of a base account and its encoding, and forgets the
// previous one otherwise.
func (c *proposalAccountCodec) remember(raw []byte, account sdk.AccountI) {
	base, ok := account.(*authtypes.BaseAccount)
	if !ok || len(raw) > maxRememberedAccountSize {
		c.raw = c.raw[:0]
		return
	}
	c.raw = append(c.raw[:0], raw...)
	c.account = *base
}

// proposalBaseAccount keeps the account inline while reusing address decoding.
// Encoding unwraps the original protobuf type, and callers own returned bytes.
type proposalBaseAccount struct {
	authtypes.BaseAccount
	codec *proposalAccountCodec
}

func (a *proposalBaseAccount) GetAddress() sdk.AccAddress {
	if a.Address != a.codec.addressText {
		a.codec.address = a.BaseAccount.GetAddress()
		a.codec.addressText = a.Address
	}
	return bytes.Clone(a.codec.address)
}
