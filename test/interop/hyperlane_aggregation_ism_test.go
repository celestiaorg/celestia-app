package interop

import (
	"crypto/ecdsa"
	"slices"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	hookkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	hooktypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/ethereum/go-ethereum/crypto"
)

// testValidator is a Hyperlane validator signing checkpoints of the origin chain's merkle tree hook.
type testValidator struct {
	privKey *ecdsa.PrivateKey
	address string
}

// checkpoint is the state of a merkle tree hook that Hyperlane validators sign.
type checkpoint struct {
	merkleTreeHook [32]byte
	root           [32]byte
	index          uint32
}

// TestHyperlaneAggregationISMInboundTransfer tests an inbound transfer from chainA to celestia
// where the celestia token is secured by an Aggregation ISM instead of a Noop ISM.
//
// The Aggregation ISM requires 2 of 3 sub-ISMs: a MessageIdMultisig ISM, a MerkleRootMultisig ISM
// (the multisig combination Hyperlane deploys by default) and a second MessageIdMultisig ISM with a
// different validator set. ChainA dispatches through a merkle tree hook and the validators sign the
// resulting on-chain checkpoint, so every sub-ISM verifies real signatures over real chain state.
func (s *HyperlaneTestSuite) TestHyperlaneAggregationISMInboundTransfer() {
	const (
		CelestiaDomainID = 69420
		ChainADomainID   = 1337
		amount           = 1000
	)

	// chainA (origin): mailbox with a merkle tree hook as required hook
	ismIDChainA := s.SetupNoopISM(s.chainA)
	mailboxIDChainA := s.SetupMailBox(s.chainA, ismIDChainA, ChainADomainID)
	merkleTreeHookID := s.SetupMerkleTreeHook(s.chainA, mailboxIDChainA)

	// celestia (destination): 2-of-3 Aggregation ISM over multisig ISMs
	validators := newTestValidators(3)
	messageIdIsmID := s.CreateMessageIdMultisigISM(s.celestia, validators[:2], 2)
	merkleRootIsmID := s.CreateMerkleRootMultisigISM(s.celestia, validators[:2], 2)
	otherMessageIdIsmID := s.CreateMessageIdMultisigISM(s.celestia, validators[2:], 1)
	aggregationIsmID := s.CreateAggregationISM(s.celestia, []util.HexAddress{messageIdIsmID, merkleRootIsmID, otherMessageIdIsmID}, 2)

	var aggregationIsm ismtypes.AggregationISM
	s.QueryISM(s.celestia, aggregationIsmID, &aggregationIsm)
	s.Require().Equal([]util.HexAddress{messageIdIsmID, merkleRootIsmID, otherMessageIdIsmID}, aggregationIsm.Modules)
	s.Require().Equal(uint32(2), aggregationIsm.Threshold)

	mailboxIDCelestia := s.SetupMailBox(s.celestia, aggregationIsmID, CelestiaDomainID)

	collatTokenID := s.CreateCollateralToken(s.chainA, ismIDChainA, mailboxIDChainA, sdk.DefaultBondDenom)
	synTokenID := s.CreateSyntheticToken(s.celestia, aggregationIsmID, mailboxIDCelestia)

	s.EnrollRemoteRouter(s.chainA, collatTokenID, CelestiaDomainID, synTokenID.String())
	s.EnrollRemoteRouter(s.celestia, synTokenID, ChainADomainID, collatTokenID.String())

	// dispatch the transfer on chainA
	res, err := s.chainA.SendMsgs(&warptypes.MsgRemoteTransfer{
		Sender:            s.chainA.SenderAccount.GetAddress().String(),
		TokenId:           collatTokenID,
		DestinationDomain: CelestiaDomainID,
		Recipient:         RecipientToHex(MakeRecipient32(s.celestia.SenderAccount.GetAddress())),
		Amount:            math.NewInt(amount),
	})
	s.Require().NoError(err)

	hypMsg := ExtractDispatchMessage(res.Events)
	s.Require().NotEmpty(hypMsg)
	message := parseHyperlaneMessage(s, hypMsg)

	// the message is the first leaf of chainA's merkle tree, so its merkle proof consists of zero hashes
	checkpoint := s.LatestCheckpoint(s.chainA, merkleTreeHookID)
	s.Require().Equal(uint32(0), checkpoint.index)
	merkleProof := util.ZeroHashes
	s.Require().Equal(checkpoint.root, util.BranchRoot(message.Id(), merkleProof, checkpoint.index))

	messageIdMetadata := signMessageIdMultisigMetadata(s, message, checkpoint, validators[:2])
	merkleRootMetadata := signMerkleRootMultisigMetadata(s, message, checkpoint, merkleProof, validators[:2])

	celestiaApp := s.GetCelestiaApp(s.celestia)
	hypDenom, err := celestiaApp.WarpKeeper.HypTokens.Get(s.celestia.GetContext(), synTokenID.GetInternalId())
	s.Require().NoError(err)

	process := func(metadata []byte) error {
		_, err := s.celestia.SendMsgs(&coretypes.MsgProcessMessage{
			MailboxId: mailboxIDCelestia,
			Relayer:   s.celestia.SenderAccount.GetAddress().String(),
			Metadata:  util.EncodeEthHex(metadata),
			Message:   hypMsg,
		})
		return err
	}
	balance := func() math.Int {
		return celestiaApp.BankKeeper.GetBalance(s.celestia.GetContext(), s.celestia.SenderAccount.GetAddress(), hypDenom.OriginDenom).Amount
	}

	// the message is rejected unless exactly `threshold` sub-ISMs verify it
	err = process(ismtypes.FormatAggregationMetadata([][]byte{messageIdMetadata, nil, nil}))
	s.Require().ErrorContains(err, "expected metadata for 2 modules, got 1")

	err = process(ismtypes.FormatAggregationMetadata([][]byte{messageIdMetadata, merkleRootMetadata, signMessageIdMultisigMetadata(s, message, checkpoint, validators[2:])}))
	s.Require().ErrorContains(err, "expected metadata for 2 modules, got 3")

	// every provided sub-ISM must verify, even if the threshold could be reached without it
	unknownSigner := signMerkleRootMultisigMetadata(s, message, checkpoint, merkleProof, newTestValidators(2))
	err = process(ismtypes.FormatAggregationMetadata([][]byte{messageIdMetadata, unknownSigner, nil}))
	s.Require().ErrorContains(err, "ism verification failed")

	// signatures attesting a different message must not verify this message
	otherMessage := message
	otherMessage.Nonce++
	err = process(ismtypes.FormatAggregationMetadata([][]byte{signMessageIdMultisigMetadata(s, otherMessage, checkpoint, validators[:2]), merkleRootMetadata, nil}))
	s.Require().ErrorContains(err, "ism verification failed")

	s.Require().True(balance().IsZero())

	// valid: the MessageIdMultisig and MerkleRootMultisig sub-ISMs verify the message
	err = process(ismtypes.FormatAggregationMetadata([][]byte{messageIdMetadata, merkleRootMetadata, nil}))
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(amount), balance())

	// a delivered message can not be processed again
	err = process(ismtypes.FormatAggregationMetadata([][]byte{messageIdMetadata, merkleRootMetadata, nil}))
	s.Require().ErrorContains(err, "already received")
	s.Require().Equal(math.NewInt(amount), balance())
}

// SetupMerkleTreeHook creates a merkle tree hook for the mailbox and sets it as the mailbox's required hook.
func (s *HyperlaneTestSuite) SetupMerkleTreeHook(chain *ibctesting.TestChain, mailboxID util.HexAddress) util.HexAddress {
	res, err := chain.SendMsgs(&hooktypes.MsgCreateMerkleTreeHook{
		Owner:     chain.SenderAccount.GetAddress().String(),
		MailboxId: mailboxID,
	})
	s.Require().NoError(err)

	var resp hooktypes.MsgCreateMerkleTreeHookResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))

	_, err = chain.SendMsgs(&coretypes.MsgSetMailbox{
		Owner:        chain.SenderAccount.GetAddress().String(),
		MailboxId:    mailboxID,
		RequiredHook: &resp.Id,
	})
	s.Require().NoError(err)

	return resp.Id
}

// LatestCheckpoint returns the latest checkpoint of the merkle tree hook as signed by Hyperlane validators.
func (s *HyperlaneTestSuite) LatestCheckpoint(chain *ibctesting.TestChain, merkleTreeHookID util.HexAddress) checkpoint {
	simapp := s.GetSimapp(chain)
	queryServer := hookkeeper.NewQueryServerImpl(&simapp.HyperlaneKeeper.PostDispatchKeeper)

	res, err := queryServer.MerkleTreeHook(chain.GetContext(), &hooktypes.QueryMerkleTreeHookRequest{Id: merkleTreeHookID.String()})
	s.Require().NoError(err)

	tree := res.MerkleTreeHook.MerkleTree
	s.Require().NotZero(tree.Count)

	return checkpoint{
		merkleTreeHook: [32]byte(merkleTreeHookID),
		root:           [32]byte(tree.Root),
		index:          tree.Count - 1,
	}
}

func (s *HyperlaneTestSuite) CreateMessageIdMultisigISM(chain *ibctesting.TestChain, validators []testValidator, threshold uint32) util.HexAddress {
	res, err := chain.SendMsgs(&ismtypes.MsgCreateMessageIdMultisigIsm{
		Creator:    chain.SenderAccount.GetAddress().String(),
		Validators: validatorAddresses(validators),
		Threshold:  threshold,
	})
	s.Require().NoError(err)

	var resp ismtypes.MsgCreateMessageIdMultisigIsmResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))

	return resp.Id
}

func (s *HyperlaneTestSuite) CreateMerkleRootMultisigISM(chain *ibctesting.TestChain, validators []testValidator, threshold uint32) util.HexAddress {
	res, err := chain.SendMsgs(&ismtypes.MsgCreateMerkleRootMultisigIsm{
		Creator:    chain.SenderAccount.GetAddress().String(),
		Validators: validatorAddresses(validators),
		Threshold:  threshold,
	})
	s.Require().NoError(err)

	var resp ismtypes.MsgCreateMerkleRootMultisigIsmResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))

	return resp.Id
}

func (s *HyperlaneTestSuite) CreateAggregationISM(chain *ibctesting.TestChain, modules []util.HexAddress, threshold uint32) util.HexAddress {
	res, err := chain.SendMsgs(&ismtypes.MsgCreateAggregationIsm{
		Creator:   chain.SenderAccount.GetAddress().String(),
		Modules:   modules,
		Threshold: threshold,
	})
	s.Require().NoError(err)

	var resp ismtypes.MsgCreateAggregationIsmResponse
	s.Require().NoError(unmarshalMsgResponses(chain.Codec, res.GetData(), &resp))

	return resp.Id
}

// QueryISM queries an ISM through the ISM query server and unpacks it into ism.
func (s *HyperlaneTestSuite) QueryISM(chain *ibctesting.TestChain, ismID util.HexAddress, ism ismtypes.HyperlaneInterchainSecurityModule) {
	celestiaApp := s.GetCelestiaApp(chain)
	queryServer := ismkeeper.NewQueryServerImpl(&celestiaApp.HyperlaneKeeper.IsmKeeper)

	res, err := queryServer.Ism(chain.GetContext(), &ismtypes.QueryIsmRequest{Id: ismID.String()})
	s.Require().NoError(err)
	s.Require().NoError(chain.Codec.Unmarshal(res.Ism.Value, ism))
}

// newTestValidators returns validators sorted by address, as required by the multisig ISMs.
func newTestValidators(count int) []testValidator {
	validators := make([]testValidator, count)
	for i := range validators {
		privKey, err := crypto.GenerateKey()
		if err != nil {
			panic(err)
		}
		address := crypto.PubkeyToAddress(privKey.PublicKey)
		validators[i] = testValidator{privKey: privKey, address: util.EncodeEthHex(address[:])}
	}

	slices.SortFunc(validators, func(a, b testValidator) int {
		if a.address < b.address {
			return -1
		}
		return 1
	})

	return validators
}

func validatorAddresses(validators []testValidator) []string {
	addresses := make([]string, len(validators))
	for i, validator := range validators {
		addresses[i] = validator.address
	}
	return addresses
}

// signDigest signs the digest of a checkpoint by all validators, in validator order.
func signDigest(s *HyperlaneTestSuite, digest [32]byte, validators []testValidator) [][]byte {
	signatures := make([][]byte, len(validators))
	for i, validator := range validators {
		signature, err := crypto.Sign(digest[:], validator.privKey)
		s.Require().NoError(err)
		signature[64] += 27
		signatures[i] = signature
	}
	return signatures
}

func signMessageIdMultisigMetadata(s *HyperlaneTestSuite, message util.HyperlaneMessage, checkpoint checkpoint, validators []testValidator) []byte {
	metadata := ismtypes.MessageIdMultisigMetadata{
		MerkleTreeHook: checkpoint.merkleTreeHook,
		MerkleRoot:     checkpoint.root,
		MerkleIndex:    checkpoint.index,
	}
	metadata.Signatures = signDigest(s, metadata.Digest(&message), validators)
	return metadata.Bytes()
}

func signMerkleRootMultisigMetadata(s *HyperlaneTestSuite, message util.HyperlaneMessage, checkpoint checkpoint, merkleProof [32][32]byte, validators []testValidator) []byte {
	metadata := ismtypes.MerkleRootMultisigMetadata{
		MerkleTreeHook:  checkpoint.merkleTreeHook,
		MessageIndex:    checkpoint.index,
		MerkleProof:     merkleProof,
		SignedIndex:     checkpoint.index,
		SignedMessageId: message.Id(),
	}
	metadata.Signatures = signDigest(s, metadata.Digest(&message), validators)
	return metadata.Bytes()
}

func parseHyperlaneMessage(s *HyperlaneTestSuite, hexMessage string) util.HyperlaneMessage {
	raw, err := util.DecodeEthHex(hexMessage)
	s.Require().NoError(err)

	message, err := util.ParseHyperlaneMessage(raw)
	s.Require().NoError(err)

	return message
}
