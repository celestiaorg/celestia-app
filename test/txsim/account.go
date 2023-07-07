package txsim

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/rs/zerolog/log"
)

const defaultFee = DefaultGasLimit * appconsts.DefaultMinGasPrice

type AccountManager struct {
	keys    keyring.Keyring
	tx      *TxClient
	query   *QueryClient
	pending []*Account

	// to protect from concurrent writes to the map
	mtx           sync.Mutex
	masterAccount *Account
	accounts      map[string]*Account
}

type Account struct {
	Address       types.AccAddress
	PubKey        cryptotypes.PubKey
	Sequence      uint64
	AccountNumber uint64
	Balance       int64
}

func NewAccountManager(ctx context.Context, keys keyring.Keyring, txClient *TxClient, queryClient *QueryClient) (*AccountManager, error) {
	records, err := keys.List()
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no accounts found in keyring")
	}

	am := &AccountManager{
		keys:     keys,
		accounts: make(map[string]*Account),
		pending:  make([]*Account, 0),
		tx:       txClient,
		query:    queryClient,
	}

	if err := am.setupMasterAccount(ctx); err != nil {
		return nil, err
	}

	log.Info().
		Str("address", am.masterAccount.Address.String()).
		Int64("balance", am.masterAccount.Balance).
		Msg("set master account")
	am.accounts[am.masterAccount.Address.String()] = am.masterAccount

	return am, nil
}

// setupMasterAccount loops through all accounts in the keyring and picks out the one with
// the highest balance as the master account. Accounts that don't yet exist on chain are
// ignored.
func (am *AccountManager) setupMasterAccount(ctx context.Context) error {
	am.mtx.Lock()
	defer am.mtx.Unlock()

	records, err := am.keys.List()
	if err != nil {
		return err
	}

	for _, record := range records {
		address, err := record.GetAddress()
		if err != nil {
			return fmt.Errorf("error getting address for account %s: %w", record.Name, err)
		}

		// search for the account on chain
		balance, err := am.getBalance(ctx, address)
		if err != nil {
			log.Err(err).Str("account", record.Name).Msg("error getting initial account balance")
			continue
		}

		// the master account is the account with the highest balance
		if am.masterAccount == nil || balance > am.masterAccount.Balance {
			accountNumber, sequence, err := am.getAccountDetails(ctx, address)
			if err != nil {
				log.Err(err).Str("account", record.Name).Msg("error getting initial account details")
				continue
			}
			pk, err := record.GetPubKey()
			if err != nil {
				return fmt.Errorf("error getting public key for account %s: %w", record.Name, err)
			}
			am.masterAccount = &Account{
				Address:       address,
				PubKey:        pk,
				Sequence:      sequence,
				AccountNumber: accountNumber,
				Balance:       balance,
			}
		}
	}

	if am.masterAccount == nil {
		return fmt.Errorf("no suitable master account found")
	}

	return nil
}

// AllocateAccounts is used by sequences to specify the number of accounts
// and the balance of each of those accounts. Not concurrently safe.
func (am *AccountManager) AllocateAccounts(n, balance int) []types.AccAddress {
	if n < 1 {
		panic("n must be greater than 0")
	}
	if balance < 1 {
		panic("balance must be greater than 0")
	}

	path := hd.CreateHDPath(types.CoinType, 0, 0).String()
	addresses := make([]types.AccAddress, n)
	for i := 0; i < n; i++ {
		record, _, err := am.keys.NewMnemonic(am.nextAccountName(), keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
		if err != nil {
			panic(err)
		}
		addresses[i], err = record.GetAddress()
		if err != nil {
			panic(err)
		}

		pk, err := record.GetPubKey()
		if err != nil {
			panic(err)
		}

		am.pending = append(am.pending, &Account{
			Address: addresses[i],
			PubKey:  pk,
			Balance: int64(balance),
		})
	}
	return addresses
}

// Submit executes on an operation. This is thread safe.
func (am *AccountManager) Submit(ctx context.Context, op Operation) error {
	for _, msg := range op.Msgs {
		if err := msg.ValidateBasic(); err != nil {
			return fmt.Errorf("error validating message: %w", err)
		}
	}

	// create the tx builder and add the messages
	builder := am.tx.Tx()
	err := builder.SetMsgs(op.Msgs...)
	if err != nil {
		return fmt.Errorf("error setting messages: %w", err)
	}

	if op.GasLimit == 0 {
		builder.SetGasLimit(DefaultGasLimit)
		builder.SetFeeAmount(types.NewCoins(types.NewInt64Coin(app.BondDenom, int64(defaultFee))))
	} else {
		builder.SetGasLimit(op.GasLimit)
		if op.GasPrice > 0 {
			builder.SetFeeAmount(types.NewCoins(types.NewInt64Coin(app.BondDenom, int64(float64(op.GasLimit)*op.GasPrice))))
		} else {
			builder.SetFeeAmount(types.NewCoins(types.NewInt64Coin(app.BondDenom, int64(float64(op.GasLimit)*appconsts.DefaultMinGasPrice))))
		}
	}

	if err := am.signTransaction(builder); err != nil {
		return err
	}

	// If the sequence specified a delay, then wait for those blocks to be produced
	if op.Delay != 0 {
		if err := am.tx.WaitForNBlocks(ctx, op.Delay); err != nil {
			return fmt.Errorf("error waiting for blocks: %w", err)
		}
	}

	// broadcast the transaction
	resp, err := am.tx.Broadcast(ctx, builder, op.Blobs)
	if err != nil {
		return fmt.Errorf("error broadcasting transaction: %w", err)
	}

	signers := builder.GetTx().GetSigners()

	// increment the sequence number for all the signers
	am.incrementSignerSequences(signers)

	log.Info().
		Int64("height", resp.Height).
		Str("signers", addrsToString(signers)).
		Str("msgs", msgsToString(op.Msgs)).
		Msg("tx committed")

	return nil
}

// Generate the pending accounts by sending the adequate funds and setting up the feegrant permissions.
// This operation is not concurrently safe.
func (am *AccountManager) GenerateAccounts(ctx context.Context) error {
	if len(am.pending) == 0 {
		return nil
	}

	msgs := make([]types.Msg, 0)
	// batch together all the messages needed to create all the accounts
	for _, acc := range am.pending {
		if am.masterAccount.Balance < acc.Balance {
			return fmt.Errorf("master account has insufficient funds")
		}

		bankMsg := bank.NewMsgSend(am.masterAccount.Address, acc.Address, types.NewCoins(types.NewInt64Coin(app.BondDenom, acc.Balance)))
		msgs = append(msgs, bankMsg)
	}

	err := am.Submit(ctx, Operation{Msgs: msgs, GasLimit: SendGasLimit * uint64(len(am.pending))})
	if err != nil {
		return fmt.Errorf("error funding accounts: %w", err)
	}

	// check that the account now exists
	for _, acc := range am.pending {
		acc.AccountNumber, acc.Sequence, err = am.getAccountDetails(ctx, acc.Address)
		if err != nil {
			return fmt.Errorf("getting account %s: %w", acc.Address, err)
		}

		// set the account
		am.accounts[acc.Address.String()] = acc
		log.Info().
			Str("address", acc.Address.String()).
			Int64("balance", acc.Balance).
			Str("pubkey", acc.PubKey.String()).
			Uint64("account number", acc.AccountNumber).
			Uint64("sequence", acc.Sequence).
			Msg("initialized account")
	}

	// update master account
	err = am.updateAccount(ctx, am.masterAccount)
	if err != nil {
		return fmt.Errorf("updating master account: %w", err)
	}

	// clear the pending accounts
	am.pending = nil
	return nil
}

func (am *AccountManager) signTransaction(builder client.TxBuilder) error {
	signers := builder.GetTx().GetSigners()
	for _, signer := range signers {
		_, ok := am.accounts[signer.String()]
		if !ok {
			return fmt.Errorf("account %s not found", signer.String())
		}
	}

	// To ensure we have the correct bytes to sign over we produce
	// a dry run of the signing data
	draftsigV2 := make([]signing.SignatureV2, len(signers))
	index := 0
	for _, signer := range signers {
		acc := am.accounts[signer.String()]
		record, err := am.keys.KeyByAddress(signer)
		if err != nil {
			return fmt.Errorf("error getting key for account %s: %w", signer.String(), err)
		}
		pk, _ := record.GetPubKey()
		if !pk.Equals(acc.PubKey) {
			return fmt.Errorf("public key (%s != %s) mismatch for account %s", pk.String(), acc.PubKey.String(), signer.String())
		}
		draftsigV2[index] = signing.SignatureV2{
			PubKey: acc.PubKey,
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
				Signature: nil,
			},
			Sequence: acc.Sequence,
		}
		index++
	}

	err := builder.SetSignatures(draftsigV2...)
	if err != nil {
		return fmt.Errorf("error setting draft signatures: %w", err)
	}

	// now we can use the data to produce the signature from each signer
	index = 0
	sigV2 := make([]signing.SignatureV2, len(signers))
	for _, signer := range signers {
		acc := am.accounts[signer.String()]
		signature, err := am.createSignature(acc, builder)
		if err != nil {
			return fmt.Errorf("error creating signature: %w", err)
		}
		sigV2[index] = signing.SignatureV2{
			PubKey: acc.PubKey,
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
				Signature: signature,
			},
			Sequence: acc.Sequence,
		}
		index++
	}

	err = builder.SetSignatures(sigV2...)
	if err != nil {
		return fmt.Errorf("error setting signatures: %w", err)
	}

	return nil
}

func (am *AccountManager) createSignature(account *Account, builder client.TxBuilder) ([]byte, error) {
	signerData := authsigning.SignerData{
		Address:       account.Address.String(),
		ChainID:       am.tx.ChainID(),
		AccountNumber: account.AccountNumber,
		Sequence:      account.Sequence,
		PubKey:        account.PubKey,
	}

	bytesToSign, err := am.tx.encCfg.TxConfig.SignModeHandler().GetSignBytes(
		signing.SignMode_SIGN_MODE_DIRECT,
		signerData,
		builder.GetTx(),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting sign bytes: %w", err)
	}

	signature, _, err := am.keys.SignByAddress(account.Address, bytesToSign)
	if err != nil {
		return nil, fmt.Errorf("error signing bytes: %w", err)
	}

	return signature, nil
}

func (am *AccountManager) updateAccount(ctx context.Context, account *Account) error {
	newBalance, err := am.getBalance(ctx, account.Address)
	if err != nil {
		return fmt.Errorf("getting account balance: %w", err)
	}
	newAccountNumber, newSequence, err := am.getAccountDetails(ctx, account.Address)
	if err != nil {
		return fmt.Errorf("getting account details: %w", err)
	}

	am.mtx.Lock()
	defer am.mtx.Unlock()
	account.Balance = newBalance
	account.AccountNumber = newAccountNumber
	account.Sequence = newSequence
	return nil
}

// getBalance returns the balance for the given address
func (am *AccountManager) getBalance(ctx context.Context, address types.AccAddress) (int64, error) {
	balanceResp, err := am.query.Bank().Balance(ctx, &bank.QueryBalanceRequest{
		Address: address.String(),
		Denom:   app.BondDenom,
	})
	if err != nil {
		return 0, fmt.Errorf("error getting balance for %s: %w", address.String(), err)
	}
	return balanceResp.GetBalance().Amount.Int64(), nil
}

// getAccountDetails returns the account number and sequence for the given address
func (am *AccountManager) getAccountDetails(ctx context.Context, address types.AccAddress) (uint64, uint64, error) {
	accountResp, err := am.query.Auth().Account(ctx, &auth.QueryAccountRequest{
		Address: address.String(),
	})
	if err != nil {
		return 0, 0, fmt.Errorf("error getting account state for %s: %w", address.String(), err)
	}

	var acc auth.AccountI
	err = am.tx.encCfg.InterfaceRegistry.UnpackAny(accountResp.Account, &acc)
	if err != nil {
		return 0, 0, fmt.Errorf("error unpacking account: %w", err)
	}

	return acc.GetAccountNumber(), acc.GetSequence(), nil
}

func (am *AccountManager) incrementSignerSequences(signers []types.AccAddress) {
	am.mtx.Lock()
	defer am.mtx.Unlock()
	for _, signer := range signers {
		am.accounts[signer.String()].Sequence++
	}
}

func (am *AccountManager) nextAccountName() string {
	return accountName(len(am.pending) + len(am.accounts))
}

func accountName(n int) string { return fmt.Sprintf("tx-sim-%d", n) }

func addrsToString(addrs []types.AccAddress) string {
	addrsStr := make([]string, len(addrs))
	for i, addr := range addrs {
		addrsStr[i] = addr.String()
	}
	return strings.Join(addrsStr, ",")
}

func msgsToString(msgs []types.Msg) string {
	msgsStr := make([]string, len(msgs))
	for i, msg := range msgs {
		msgsStr[i] = types.MsgTypeURL(msg)
	}
	return strings.Join(msgsStr, ",")
}
