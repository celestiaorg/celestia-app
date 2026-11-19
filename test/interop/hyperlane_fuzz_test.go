package interop

import (
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"slices"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/params"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	hyperlaneFuzzLocalDomain  = 69420
	hyperlaneFuzzRemoteDomain = 1337
	// hyperlaneFuzzDeposit is the collateral locked in the funded token.
	hyperlaneFuzzDeposit = 1000
	// The multisig ISM has hyperlaneFuzzValidators validators and accepts
	// hyperlaneFuzzThreshold signatures.
	hyperlaneFuzzValidators = 3
	hyperlaneFuzzThreshold  = 2
	// hyperlaneFuzzSignedLen is the part of merkle-root multisig metadata that
	// the ISM checks: everything up to the threshold signatures.
	hyperlaneFuzzSignedLen = 1096 + 65*hyperlaneFuzzThreshold
)

// The tokens a message can be addressed to. FuzzHyperlaneProcessMessage
// picks one of the tokens before toMultisigSynthetic, and
// FuzzHyperlaneMultisigISM sends only to toMultisigSynthetic.
const (
	toFundedCollateral  = iota // collateral token for utia holding hyperlaneFuzzDeposit
	toEmptyCollateral          // another collateral token for utia, holding nothing
	toSynthetic                // synthetic token
	toUnknown                  // an address that is not a token
	toMultisigSynthetic        // synthetic token behind a merkle-root multisig ISM
	numRecipients
)

var hyperlaneFixture *hyperlaneFuzzFixture

// FuzzHyperlaneProcessMessage delivers arbitrary Hyperlane messages to
// celestia through MsgProcessMessage: mailbox checks, a noop ISM, and the
// warp handler that releases collateral or mints synthetic tokens. Anyone can
// create the ISM, mailbox and warp tokens the fixture uses, so every field of
// the message is attacker controlled.
//
// No message may panic, and two runs of the same message on the same state
// must give the same result, events, gas and balances. A message that
// succeeds must come from the enrolled router for the token, move exactly the
// payload amount of the addressed token and nothing else, and be rejected
// when replayed. Collateral tokens for the same denom share the warp module
// account, so releasing from one must never touch another's collateral.
func FuzzHyperlaneProcessMessage(f *testing.F) {
	router := hyperlaneFuzzRouter()
	receiver := hyperlaneFuzzReceiver()
	for recipient := range uint8(toMultisigSynthetic) {
		for _, body := range hyperlaneFuzzBodies() {
			f.Add(coretypes.MESSAGE_VERSION, uint32(1), uint32(hyperlaneFuzzRemoteDomain), router[:], uint32(hyperlaneFuzzLocalDomain), recipient, body)
		}
	}
	valid := hyperlaneFuzzPayload(receiver, big.NewInt(1))
	f.Add(coretypes.MESSAGE_VERSION, uint32(1), uint32(hyperlaneFuzzLocalDomain), router[:], uint32(hyperlaneFuzzLocalDomain), uint8(toSynthetic), valid)
	f.Add(coretypes.MESSAGE_VERSION, uint32(1), uint32(hyperlaneFuzzRemoteDomain), receiver, uint32(hyperlaneFuzzLocalDomain), uint8(toSynthetic), valid)
	f.Add(coretypes.MESSAGE_VERSION, uint32(1), uint32(hyperlaneFuzzRemoteDomain), router[:], uint32(hyperlaneFuzzRemoteDomain), uint8(toSynthetic), valid)
	f.Add(uint8(0), uint32(1), uint32(hyperlaneFuzzRemoteDomain), router[:], uint32(hyperlaneFuzzLocalDomain), uint8(toSynthetic), valid)

	f.Fuzz(func(t *testing.T, version uint8, nonce, origin uint32, sender []byte, destination uint32, recipient uint8, body []byte) {
		if len(sender)+len(body) > 1<<20 {
			return
		}
		fx := hyperlaneFuzzSetup(t)
		in := hyperlaneFuzzInput{
			version: version, nonce: nonce, origin: origin, destination: destination,
			recipient: int(recipient % toMultisigSynthetic), body: body,
		}
		copy(in.sender[:], sender)
		c := fx.newCase(in, nil, nil)
		first := fx.run(t, c)
		second := fx.run(t, c)
		require.Equal(t, first, second, "the same message on the same state gave different results")
	})
}

// FuzzHyperlaneMultisigISM delivers messages to a synthetic token behind a
// merkle-root multisig ISM with hyperlaneFuzzThreshold of
// hyperlaneFuzzValidators validators. The fuzzer picks which validators sign
// and in what order, and a mutation of the signed metadata: bytes written at
// an offset, optionally dropping the rest.
//
// On top of the checks of FuzzHyperlaneProcessMessage, an accepted message
// must carry the signed part of the metadata unchanged. For unmutated
// metadata, the ISM must accept exactly when the first threshold signatures
// come from distinct validators in validator order.
func FuzzHyperlaneMultisigISM(f *testing.F) {
	ordered := []byte{0, 1}
	for _, body := range hyperlaneFuzzBodies() {
		f.Add(uint32(1), body, ordered, uint16(0), []byte(nil), false)
	}
	valid := hyperlaneFuzzPayload(hyperlaneFuzzReceiver(), big.NewInt(1))
	for _, signers := range [][]byte{nil, {0}, {1, 2}, {0, 2}, {1, 0}, {0, 0}, {2, 1, 0}, {0, 1, 2}} {
		f.Add(uint32(1), valid, signers, uint16(0), []byte(nil), false)
	}
	// Mutations of the signed metadata: the merkle tree hook, the message
	// index, the signed message id, the proof, the signed index, a signature,
	// a recovery id, an extra signature, and a dropped signature.
	mutations := []struct {
		at       uint16
		with     []byte
		truncate bool
	}{
		{0, []byte{1}, false},
		{35, []byte{1}, false},
		{36, []byte{1}, false},
		{100, []byte{1}, false},
		{1095, []byte{1}, false},
		{1096, []byte{1}, false},
		{1096 + 64, []byte{29}, false},
		{hyperlaneFuzzSignedLen, make([]byte, 65), false},
		{1096 + 65, nil, true},
		{1096 + 65, []byte{0}, true},
	}
	for _, m := range mutations {
		f.Add(uint32(1), valid, ordered, m.at, m.with, m.truncate)
	}

	f.Fuzz(func(t *testing.T, nonce uint32, body, signers []byte, mutateAt uint16, mutation []byte, truncate bool) {
		if len(body)+len(mutation) > 1<<20 || len(signers) > 2*hyperlaneFuzzValidators {
			return
		}
		fx := hyperlaneFuzzSetup(t)
		in := hyperlaneFuzzInput{
			version: coretypes.MESSAGE_VERSION, nonce: nonce, origin: hyperlaneFuzzRemoteDomain,
			sender: hyperlaneFuzzRouter(), destination: hyperlaneFuzzLocalDomain,
			recipient: toMultisigSynthetic, body: body,
		}
		message := fx.message(in)
		order := make([]int, len(signers))
		for i, s := range signers {
			order[i] = int(s) % hyperlaneFuzzValidators
		}
		signed := fx.sign(t, message, order)
		metadata := hyperlaneFuzzMutate(signed, mutateAt, mutation, truncate)
		c := fx.newCase(in, signed, metadata)
		first := fx.run(t, c)
		second := fx.run(t, c)
		require.Equal(t, first, second, "the same message on the same state gave different results")

		if len(mutation) == 0 && !truncate {
			inOrder := len(order) >= hyperlaneFuzzThreshold && order[0] < order[1]
			ctx, _ := fx.ctx.CacheContext()
			ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			verified, err := fx.app.HyperlaneKeeper.Verify(ctx, fx.multisigISM, metadata, message)
			require.Equal(t, inOrder, err == nil && verified, "signers %v: verified %v, err %v", order, verified, err)
		}
	})
}

// TestHyperlaneFuzzFixtureAcceptsValidMessages checks that valid messages
// reach the warp handler and succeed. The fuzz targets accept any rejected
// message, so if valid messages stopped succeeding, after a hyperlane-cosmos
// upgrade for example, they would pass while checking nothing.
func TestHyperlaneFuzzFixtureAcceptsValidMessages(t *testing.T) {
	fx := newHyperlaneFuzzFixture(t)
	for _, recipient := range []int{toFundedCollateral, toSynthetic, toMultisigSynthetic} {
		in := hyperlaneFuzzInput{
			version: coretypes.MESSAGE_VERSION, nonce: 1, origin: hyperlaneFuzzRemoteDomain,
			sender: hyperlaneFuzzRouter(), destination: hyperlaneFuzzLocalDomain,
			recipient: recipient, body: hyperlaneFuzzPayload(hyperlaneFuzzReceiver(), big.NewInt(1)),
		}
		var signed []byte
		if recipient == toMultisigSynthetic {
			signed = fx.sign(t, fx.message(in), []int{0, 1})
		}
		res := fx.run(t, fx.newCase(in, signed, signed))
		require.Empty(t, res.Err, "message to recipient %d", recipient)
	}
}

func hyperlaneFuzzSetup(t *testing.T) *hyperlaneFuzzFixture {
	if hyperlaneFixture == nil {
		hyperlaneFixture = newHyperlaneFuzzFixture(t)
	}
	return hyperlaneFixture
}

// hyperlaneFuzzFixture holds celestia with a noop ISM, a merkle-root
// multisig ISM, a mailbox, and four warp tokens enrolled with the same remote
// router. Behind the noop ISM: a collateral token for utia with
// hyperlaneFuzzDeposit locked, a second collateral token for utia with nothing
// locked, and a synthetic token. Behind the multisig ISM: a second synthetic
// token. Every run works on a cache of ctx, so the fixture never changes and
// is shared by all inputs.
type hyperlaneFuzzFixture struct {
	app         *app.App
	ctx         sdk.Context
	mailbox     util.HexAddress
	multisigISM util.HexAddress
	tokens      [numRecipients]util.HexAddress
	// syntheticDenoms are the denoms of toSynthetic and toMultisigSynthetic.
	syntheticDenoms [2]string
	// validators are the multisig ISM's keys, sorted by address as the ISM
	// requires.
	validators []*ecdsa.PrivateKey
	relayer    string
	base       hyperlaneFuzzState
}

func newHyperlaneFuzzFixture(t *testing.T) *hyperlaneFuzzFixture {
	_, celestia, _, _ := SetupTest(t)
	s := &HyperlaneTestSuite{celestia: celestia}
	s.SetT(t)
	celestiaApp := s.GetCelestiaApp(celestia)

	sender := celestia.SenderAccount.GetAddress()
	utia := sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 1_000_000))
	require.NoError(t, celestiaApp.BankKeeper.MintCoins(celestia.GetContext(), minttypes.ModuleName, utia))
	require.NoError(t, celestiaApp.BankKeeper.SendCoinsFromModuleToAccount(celestia.GetContext(), minttypes.ModuleName, sender, utia))

	validators := make([]*ecdsa.PrivateKey, hyperlaneFuzzValidators)
	for i := range validators {
		key, err := crypto.ToECDSA(bytes.Repeat([]byte{byte(i + 1)}, 32))
		require.NoError(t, err)
		validators[i] = key
	}
	slices.SortFunc(validators, func(a, b *ecdsa.PrivateKey) int {
		return crypto.PubkeyToAddress(a.PublicKey).Cmp(crypto.PubkeyToAddress(b.PublicKey))
	})
	addresses := make([]string, len(validators))
	for i, key := range validators {
		addresses[i] = crypto.PubkeyToAddress(key.PublicKey).Hex()
	}
	res, err := celestia.SendMsgs(&ismtypes.MsgCreateMerkleRootMultisigIsm{
		Creator:    sender.String(),
		Validators: addresses,
		Threshold:  hyperlaneFuzzThreshold,
	})
	require.NoError(t, err)
	var multisigISM ismtypes.MsgCreateMerkleRootMultisigIsmResponse
	require.NoError(t, unmarshalMsgResponses(celestia.Codec, res.GetData(), &multisigISM))

	ism := s.SetupNoopISM(celestia)
	mailbox := s.SetupMailBox(celestia, ism, hyperlaneFuzzLocalDomain)
	var tokens [numRecipients]util.HexAddress
	tokens[toFundedCollateral] = s.CreateCollateralToken(celestia, ism, mailbox, params.BondDenom)
	tokens[toEmptyCollateral] = s.CreateCollateralToken(celestia, ism, mailbox, params.BondDenom)
	tokens[toSynthetic] = s.CreateSyntheticToken(celestia, ism, mailbox)
	tokens[toMultisigSynthetic] = s.CreateSyntheticToken(celestia, multisigISM.Id, mailbox)
	tokens[toUnknown] = util.CreateMockHexAddress("unknown", 1)
	router := hyperlaneFuzzRouter()
	for i, token := range tokens {
		if i != toUnknown {
			s.EnrollRemoteRouter(celestia, token, hyperlaneFuzzRemoteDomain, router.String())
		}
	}

	_, err = celestia.SendMsgs(&warptypes.MsgRemoteTransfer{
		Sender:            sender.String(),
		TokenId:           tokens[toFundedCollateral],
		DestinationDomain: hyperlaneFuzzRemoteDomain,
		Recipient:         router,
		Amount:            math.NewInt(hyperlaneFuzzDeposit),
	})
	require.NoError(t, err)

	ctx := celestia.GetContext()
	fx := &hyperlaneFuzzFixture{
		app:         celestiaApp,
		ctx:         ctx,
		mailbox:     mailbox,
		multisigISM: multisigISM.Id,
		tokens:      tokens,
		validators:  validators,
		relayer:     sender.String(),
	}
	for i, recipient := range []int{toSynthetic, toMultisigSynthetic} {
		token, err := celestiaApp.WarpKeeper.HypTokens.Get(ctx, tokens[recipient].GetInternalId())
		require.NoError(t, err)
		fx.syntheticDenoms[i] = token.OriginDenom
	}
	fx.base = fx.state(t, ctx)
	require.Equal(t, "1000", fx.base.FundedCollateral)
	require.Equal(t, "0", fx.base.EmptyCollateral)
	return fx
}

// hyperlaneFuzzRouter is the remote router enrolled for every token: the only
// sender a message to a token may come from.
func hyperlaneFuzzRouter() util.HexAddress {
	return util.CreateMockHexAddress("router", 1)
}

func hyperlaneFuzzReceiver() []byte {
	receiver := make([]byte, 32)
	receiver[31] = 1
	return receiver
}

// hyperlaneFuzzBodies are warp transfer bodies for the seeds: amounts around
// the deposit and the largest amount, recipients that are a cosmos account, a
// 32-byte address and the warp module account, and bodies too short to parse.
func hyperlaneFuzzBodies() [][]byte {
	receiver := hyperlaneFuzzReceiver()
	router := hyperlaneFuzzRouter()
	warpModule := make([]byte, 32)
	copy(warpModule[12:], authtypes.NewModuleAddress(warptypes.ModuleName))
	return [][]byte{
		hyperlaneFuzzPayload(receiver, big.NewInt(1)),
		hyperlaneFuzzPayload(receiver, big.NewInt(hyperlaneFuzzDeposit)),
		hyperlaneFuzzPayload(receiver, big.NewInt(hyperlaneFuzzDeposit+1)),
		hyperlaneFuzzPayload(receiver, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))),
		hyperlaneFuzzPayload(router[:], big.NewInt(1)),
		hyperlaneFuzzPayload(warpModule, big.NewInt(1)),
		receiver,
		nil,
	}
}

// hyperlaneFuzzPayload encodes a warp transfer body: a 32-byte recipient and
// a 32-byte big-endian amount.
func hyperlaneFuzzPayload(recipient []byte, amount *big.Int) []byte {
	body := make([]byte, 64)
	copy(body[32-len(recipient):32], recipient)
	amount.FillBytes(body[32:])
	return body
}

// hyperlaneFuzzMutate writes mutation into signed at mutateAt. With truncate,
// the bytes of signed after the mutation are dropped.
func hyperlaneFuzzMutate(signed []byte, mutateAt uint16, mutation []byte, truncate bool) []byte {
	if len(mutation) == 0 && !truncate {
		return signed
	}
	at := int(mutateAt) % (len(signed) + 1)
	out := append(slices.Clone(signed[:at]), mutation...)
	if end := at + len(mutation); !truncate && end < len(signed) {
		out = append(out, signed[end:]...)
	}
	return out
}

type hyperlaneFuzzInput struct {
	version                    uint8
	nonce, origin, destination uint32
	sender                     util.HexAddress
	recipient                  int
	body                       []byte
}

func (fx *hyperlaneFuzzFixture) message(in hyperlaneFuzzInput) util.HyperlaneMessage {
	return util.HyperlaneMessage{
		Version:     in.version,
		Nonce:       in.nonce,
		Origin:      in.origin,
		Sender:      in.sender,
		Destination: in.destination,
		Recipient:   fx.tokens[in.recipient],
		Body:        in.body,
	}
}

// hyperlaneFuzzCase is the message one input delivers. signed is the metadata
// the validators signed, set only for toMultisigSynthetic.
type hyperlaneFuzzCase struct {
	in               hyperlaneFuzzInput
	msg              *coretypes.MsgProcessMessage
	signed, metadata []byte
}

func (fx *hyperlaneFuzzFixture) newCase(in hyperlaneFuzzInput, signed, metadata []byte) hyperlaneFuzzCase {
	return hyperlaneFuzzCase{
		in:       in,
		signed:   signed,
		metadata: metadata,
		msg: &coretypes.MsgProcessMessage{
			MailboxId: fx.mailbox,
			Relayer:   fx.relayer,
			Metadata:  util.EncodeEthHex(metadata),
			Message:   util.EncodeEthHex(fx.message(in).Bytes()),
		},
	}
}

// sign returns merkle-root multisig metadata for message with one signature
// from each validator in order. The proof is for a tree in which the message
// is at index 0; the ISM checks only that the validators signed the root it
// computes.
func (fx *hyperlaneFuzzFixture) sign(t *testing.T, message util.HyperlaneMessage, order []int) []byte {
	t.Helper()
	metadata := ismtypes.MerkleRootMultisigMetadata{
		MerkleTreeHook:  common.HexToHash("0x01"),
		SignedMessageId: message.Id(),
	}
	digest := metadata.Digest(&message)
	for _, i := range order {
		sig, err := crypto.Sign(digest[:], fx.validators[i])
		require.NoError(t, err)
		sig[64] += 27
		metadata.Signatures = append(metadata.Signatures, sig)
	}
	return metadata.Bytes()
}

// hyperlaneFuzzState is the part of state a message may change. Amounts are
// strings so that equal values compare equal.
type hyperlaneFuzzState struct {
	FundedCollateral        string
	EmptyCollateral         string
	WarpModuleUtia          string
	UtiaSupply              string
	SyntheticSupply         string
	MultisigSyntheticSupply string
}

func (fx *hyperlaneFuzzFixture) state(t *testing.T, ctx sdk.Context) hyperlaneFuzzState {
	t.Helper()
	collateral := func(i int) string {
		token, err := fx.app.WarpKeeper.HypTokens.Get(ctx, fx.tokens[i].GetInternalId())
		require.NoError(t, err)
		return token.CollateralBalance.String()
	}
	return hyperlaneFuzzState{
		FundedCollateral:        collateral(toFundedCollateral),
		EmptyCollateral:         collateral(toEmptyCollateral),
		WarpModuleUtia:          fx.app.BankKeeper.GetBalance(ctx, authtypes.NewModuleAddress(warptypes.ModuleName), params.BondDenom).Amount.String(),
		UtiaSupply:              fx.app.BankKeeper.GetSupply(ctx, params.BondDenom).Amount.String(),
		SyntheticSupply:         fx.app.BankKeeper.GetSupply(ctx, fx.syntheticDenoms[0]).Amount.String(),
		MultisigSyntheticSupply: fx.app.BankKeeper.GetSupply(ctx, fx.syntheticDenoms[1]).Amount.String(),
	}
}

// hyperlaneFuzzResult is everything a run returned, emitted or stored. Two
// runs of the same input on the same state must produce equal results.
type hyperlaneFuzzResult struct {
	Err     string
	Events  []abci.Event
	GasUsed uint64
	State   hyperlaneFuzzState
}

func (fx *hyperlaneFuzzFixture) run(t *testing.T, c hyperlaneFuzzCase) hyperlaneFuzzResult {
	t.Helper()
	ctx, _ := fx.ctx.CacheContext()
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	var res hyperlaneFuzzResult
	// Like a delivered tx, a failed message leaves no writes or events.
	res.Events, res.Err = fx.deliver(ctx, c.msg)
	res.GasUsed = ctx.GasMeter().GasConsumed()
	res.State = fx.state(t, ctx)
	if res.Err == "" {
		fx.requireAccepted(t, ctx, c, res.State)
		_, replayErr := fx.deliver(ctx, c.msg)
		require.Contains(t, replayErr, "already received", "a replayed message must be rejected")
	} else {
		require.Equal(t, fx.base, res.State)
	}
	return res
}

func (fx *hyperlaneFuzzFixture) deliver(ctx sdk.Context, msg sdk.Msg) ([]abci.Event, string) {
	msgCtx, write := ctx.CacheContext()
	result, err := fx.app.MsgServiceRouter().Handler(msg)(msgCtx, msg)
	if err != nil {
		return nil, err.Error()
	}
	write()
	return result.Events, ""
}

// requireAccepted checks a message that succeeded: it came from the enrolled
// router to a token on this mailbox, carried the metadata the validators
// signed when its token needs it, and moved the payload amount of that token
// only, to the payload recipient.
func (fx *hyperlaneFuzzFixture) requireAccepted(t *testing.T, ctx sdk.Context, c hyperlaneFuzzCase, got hyperlaneFuzzState) {
	t.Helper()
	in := c.in
	if in.recipient == toMultisigSynthetic {
		require.GreaterOrEqual(t, len(c.signed), hyperlaneFuzzSignedLen, "the multisig ISM accepted fewer signatures than its threshold")
		require.True(t, bytes.HasPrefix(c.metadata, c.signed[:hyperlaneFuzzSignedLen]), "the multisig ISM accepted metadata whose signed part changed")
	}
	require.Equal(t, coretypes.MESSAGE_VERSION, in.version)
	require.EqualValues(t, hyperlaneFuzzLocalDomain, in.destination)
	require.EqualValues(t, hyperlaneFuzzRemoteDomain, in.origin)
	require.Equal(t, hyperlaneFuzzRouter(), in.sender)
	require.NotEqual(t, toUnknown, in.recipient)

	payload, err := warptypes.ParseWarpPayload(in.body)
	require.NoError(t, err)
	amount := math.NewIntFromBigInt(payload.Amount())
	sub := func(a string) string { v, _ := math.NewIntFromString(a); return v.Sub(amount).String() }
	add := func(a string) string { v, _ := math.NewIntFromString(a); return v.Add(amount).String() }

	want := fx.base
	denom := params.BondDenom
	switch in.recipient {
	case toFundedCollateral:
		want.FundedCollateral = sub(want.FundedCollateral)
		want.WarpModuleUtia = sub(want.WarpModuleUtia)
	case toEmptyCollateral:
		want.EmptyCollateral = sub(want.EmptyCollateral)
		want.WarpModuleUtia = sub(want.WarpModuleUtia)
	case toSynthetic:
		want.SyntheticSupply = add(want.SyntheticSupply)
		denom = fx.syntheticDenoms[0]
	case toMultisigSynthetic:
		want.MultisigSyntheticSupply = add(want.MultisigSyntheticSupply)
		denom = fx.syntheticDenoms[1]
	}
	require.Equal(t, want, got)

	recipient := payload.GetCosmosAccount()
	before := fx.app.BankKeeper.GetBalance(fx.ctx, recipient, denom).Amount
	after := fx.app.BankKeeper.GetBalance(ctx, recipient, denom).Amount
	require.Equal(t, amount.String(), after.Sub(before).String(), "the payload recipient must receive the payload amount")
}
