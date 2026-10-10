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

type proposalAccountCodec struct {
	collectionscodec.ValueCodec[sdk.AccountI]
	raw     []byte
	account authtypes.BaseAccount
	valid   bool
}

func (c *proposalAccountCodec) Decode(raw []byte) (sdk.AccountI, error) {
	if c.valid && bytes.Equal(raw, c.raw) {
		// Ante changes sequence and replaces the PubKey field; it does not
		// mutate the public key itself. Give each caller its own account.
		account := c.account
		return &account, nil
	}
	account, err := c.ValueCodec.Decode(raw)
	if err != nil {
		return nil, err
	}
	base, ok := account.(*authtypes.BaseAccount)
	if ok && len(raw) <= 4096 {
		c.raw = append(c.raw[:0], raw...)
		c.account = *base
		c.valid = true
	} else {
		c.valid = false
	}
	return account, nil
}

// Encode primes the same byte-checked cache for the account just written.
func (c *proposalAccountCodec) Encode(account sdk.AccountI) ([]byte, error) {
	raw, err := c.ValueCodec.Encode(account)
	if err != nil {
		return nil, err
	}
	base, ok := account.(*authtypes.BaseAccount)
	if ok && len(raw) <= 4096 {
		c.raw = append(c.raw[:0], raw...)
		c.account = *base
		c.valid = true
	} else {
		c.valid = false
	}
	return raw, nil
}
