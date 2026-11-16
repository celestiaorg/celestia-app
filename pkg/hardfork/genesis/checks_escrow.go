package genesis

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"cosmossdk.io/math"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v8/modules/core/types"
)

// FibreEscrow is the escrow of one owner in one denom; Balance == Available + Pending withdrawals.
type FibreEscrow struct {
	Owner, Denom                string
	Balance, Available, Pending math.Int
}

// TransferEscrow is the balance of the escrow account of one transfer channel.
type TransferEscrow struct {
	Port, Channel string
	Balance       sdk.Coins
}

// CheckGovNulls rejects missing or null gov params, a deposit param without an amount and null proposals, messages,
// deposits, votes and vote options, which ValidateGenesis, InitGenesis or EndBlock dereference. Run it before any of them.
func CheckGovNulls(g *Genesis) error {
	raw, ok := g.Module(govtypes.ModuleName)
	if !ok {
		return nil // decodeModule reports it
	}
	var gs struct {
		Params    map[string]json.RawMessage
		Proposals []*struct{ Messages []*struct{} }
		Deposits  []*struct{}
		Votes     []*struct{ Options []*struct{} }
	}
	if err := json.Unmarshal(raw, &gs); err != nil {
		return &CheckError{Step: stepRecords, Module: govtypes.ModuleName, Got: err.Error()}
	}
	null := func(field string, i ...any) error {
		return &CheckError{Step: stepRecords, Module: govtypes.ModuleName, Field: fmt.Sprintf(field, i...), Want: "set", Got: "null"}
	}
	if gs.Params == nil {
		return null("params")
	}
	// The codec accepts both field names and reports a list that does not decode.
	for _, name := range []string{"min_deposit", "minDeposit", "expedited_min_deposit", "expeditedMinDeposit"} {
		var coins []struct{ Amount any }
		if json.Unmarshal(gs.Params[name], &coins) == nil {
			if i := slices.IndexFunc(coins, func(c struct{ Amount any }) bool { return c.Amount == nil }); i >= 0 {
				return null("params.%s[%d].amount", name, i)
			}
		}
	}
	for i, p := range gs.Proposals {
		if p == nil {
			return null("proposals[%d]", i)
		}
		if j := slices.Index(p.Messages, nil); j >= 0 {
			return null("proposals[%d].messages[%d]", i, j)
		}
	}
	if i := slices.Index(gs.Deposits, nil); i >= 0 {
		return null("deposits[%d]", i)
	}
	for i, v := range gs.Votes {
		if v == nil {
			return null("votes[%d]", i)
		}
		if j := slices.Index(v.Options, nil); j >= 0 {
			return null("votes[%d].options[%d]", i, j)
		}
	}
	return nil
}

// checkGov checks proposals, votes and deposits, requires bal(gov) to cover every deposit and returns the surplus.
func checkGov(cdc codec.JSONCodec, g *Genesis, bal sdk.Coins, opts Options) (sdk.Coins, error) {
	const module = govtypes.ModuleName
	var gs govv1.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return nil, err
	}
	blocked, err := blockedAddrs(opts.BlockedAddresses)
	if err != nil {
		return nil, err
	}
	if err := checkProposals(&gs, opts.ExportBlockTime); err != nil {
		return nil, err
	}
	if err := checkVotes(gs.Votes); err != nil {
		return nil, err
	}
	deposits := intSum{}
	seen := make(map[[2]string]int, len(gs.Deposits))
	for i, d := range gs.Deposits {
		depositor, err := decodeAddr(accCodec, nil, module, "deposits", i, "depositor", d.Depositor)
		if err != nil {
			return nil, err
		}
		if j, ok := seen[[2]string{fmt.Sprint(d.ProposalId), depositor}]; ok {
			return nil, duplicateError(module, "deposits", i, j, d.Depositor)
		}
		seen[[2]string{fmt.Sprint(d.ProposalId), depositor}] = i
		// A refund or burn to a blocked address fails in EndBlock.
		if blocked[depositor] {
			return nil, &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("deposits[%d].depositor", i), Want: "not a blocked address", Got: d.Depositor}
		}
		if err := validCoins(d.Amount); err != nil {
			return nil, &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("deposits[%d].amount", i), Want: "valid positive coins", Got: err.Error()}
		}
		if err := deposits.addCoins(d.Amount); err != nil {
			return nil, overflowError(module, "deposits", err)
		}
	}
	return coversCoins(module, "balance", deposits.coins(), bal)
}

// blockedAddrs decodes the app blocklist; an empty one means the caller did not pass it.
func blockedAddrs(addrs map[string]bool) (map[string]bool, error) {
	if len(addrs) == 0 {
		return nil, &CheckError{Step: stepInput, Field: "blocked-addresses", Want: "the app blocklist", Got: "empty"}
	}
	blocked := make(map[string]bool, len(addrs))
	for _, addr := range slices.Sorted(maps.Keys(addrs)) {
		bz, err := accCodec.StringToBytes(addr)
		if err != nil {
			return nil, &CheckError{Step: stepInput, Field: "blocked-addresses", Want: "valid addresses", Got: err.Error()}
		}
		blocked[string(bz)] = blocked[string(bz)] || addrs[addr]
	}
	return blocked, nil
}

// checkProposals requires the times InitGenesis and EndBlock dereference by status and a valid total deposit, applies
// the T sanity bound to submit times and requires starting_proposal_id above every proposal id.
func checkProposals(gs *govv1.GenesisState, blockTime time.Time) error {
	const module = govtypes.ModuleName
	var maxID uint64
	for i, p := range gs.Proposals {
		field, missing := fmt.Sprintf("proposals[%d]", i), ""
		switch {
		case p.SubmitTime == nil:
			missing = "submit_time"
		case p.Status == govv1.StatusDepositPeriod && p.DepositEndTime == nil:
			missing = "deposit_end_time"
		case p.Status == govv1.StatusVotingPeriod && p.VotingEndTime == nil:
			missing = "voting_end_time"
		case p.Status == govv1.StatusVotingPeriod && p.Expedited && p.VotingStartTime == nil:
			missing = "voting_start_time" // a failed expedited tally extends the period from it
		}
		if missing != "" {
			return &CheckError{Step: stepRecords, Module: module, Field: field + "." + missing, Want: "set", Got: "missing"}
		}
		// EndBlock passes it to sdk.NewCoins, which panics on invalid coins.
		if err := validCoins(p.TotalDeposit); err != nil {
			return &CheckError{Step: stepRecords, Module: module, Field: field + ".total_deposit", Want: "valid positive coins", Got: err.Error()}
		}
		if err := notAfter(module, field+".submit_time", *p.SubmitTime, blockTime); err != nil {
			return err
		}
		maxID = max(maxID, p.Id)
	}
	if gs.StartingProposalId <= maxID {
		return &CheckError{Step: stepRecords, Module: module, Field: "starting_proposal_id", Want: fmt.Sprintf("> %d (the largest proposal id)", maxID), Got: fmt.Sprint(gs.StartingProposalId)}
	}
	return nil
}

// checkVotes rejects duplicate (proposal, voter) pairs and options that MsgVoteWeighted would refuse, which the tally reads unchecked.
func checkVotes(votes []*govv1.Vote) error {
	const module = govtypes.ModuleName
	seen := make(map[[2]string]int, len(votes))
	for i, v := range votes {
		voter, err := decodeAddr(accCodec, nil, module, "votes", i, "voter", v.Voter)
		if err != nil {
			return err
		}
		if j, ok := seen[[2]string{fmt.Sprint(v.ProposalId), voter}]; ok {
			return duplicateError(module, "votes", i, j, v.Voter)
		}
		seen[[2]string{fmt.Sprint(v.ProposalId), voter}] = i
		if !validVoteOptions(v.Options) {
			return &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("votes[%d].options", i), Want: "distinct valid options with weights summing to 1", Got: govv1.WeightedVoteOptions(v.Options).String()}
		}
	}
	return nil
}

// validVoteOptions applies the MsgVoteWeighted rules: distinct valid options, each weight in (0, 1], summing to 1.
func validVoteOptions(opts []*govv1.WeightedVoteOption) bool {
	total, used := math.LegacyZeroDec(), make(map[govv1.VoteOption]bool, len(opts))
	for _, o := range opts {
		w, err := math.LegacyNewDecFromStr(o.Weight)
		if err != nil || !w.IsPositive() || w.GT(math.LegacyOneDec()) || !govv1.ValidVoteOption(o.Option) || used[o.Option] {
			return false
		}
		used[o.Option], total = true, total.Add(w)
	}
	return total.Equal(math.LegacyOneDec())
}

// checkFibre aggregates escrows per decoded owner as Migrate1to2 does, requires each to equal its available balance
// plus pending withdrawals and bal(fibre) to cover the escrow balances, and returns the escrows and the surplus.
func checkFibre(cdc codec.JSONCodec, g *Genesis, bal sdk.Coins, blockTime time.Time) ([]FibreEscrow, sdk.Coins, error) {
	const module = fibretypes.ModuleName
	var gs fibretypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return nil, nil, err
	}
	// The rules InitGenesis enforces, including that every withdrawal has an escrow of its denom.
	if err := gs.ValidateRecords(); err != nil {
		return nil, nil, &CheckError{Step: stepRecords, Module: module, Want: "valid records", Got: err.Error()}
	}
	if err := notAfter(module, "promise_freshness_floor", gs.PromiseFreshnessFloor, blockTime); err != nil {
		return nil, nil, err
	}
	for i, p := range gs.ProcessedPayments {
		if err := notAfter(module, fmt.Sprintf("processed_payments[%d].processed_at", i), p.ProcessedAt, blockTime); err != nil {
			return nil, nil, err
		}
	}
	escrows, sum, err := sumFibre(&gs, blockTime)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(escrows, func(a, b FibreEscrow) int { return strings.Compare(a.Owner, b.Owner) }) // one denom per owner
	for _, e := range escrows {
		// Withdrawal requests move funds from available to pending; payments and processing lower both sides together.
		reserved, err := e.Available.SafeAdd(e.Pending)
		if err != nil {
			return nil, nil, overflowError(module, "withdrawals", err)
		}
		if !reserved.Equal(e.Balance) {
			return nil, nil, &CheckError{Step: stepConservation, Module: module, Field: fmt.Sprintf("escrow_accounts[%s].balance[%s]", e.Owner, e.Denom), Want: reserved.String() + " (available + pending)", Got: e.Balance.String()}
		}
	}
	surplus, err := coversCoins(module, "balance", sum.coins(), bal)
	return escrows, surplus, err
}

// sumFibre totals escrows and withdrawals per canonical owner and denom, and escrow balances per denom.
func sumFibre(gs *fibretypes.GenesisState, blockTime time.Time) ([]FibreEscrow, intSum, error) {
	const module = fibretypes.ModuleName
	byKey := make(map[[2]string]*FibreEscrow, len(gs.EscrowAccounts))
	sum := intSum{}
	for i, a := range gs.EscrowAccounts {
		owner, err := decodeAddr(accCodec, nil, module, "escrow_accounts", i, "signer", a.Signer)
		if err != nil {
			return nil, nil, err
		}
		// Migrate1to2 refunds other denoms at import and fails on a blocked owner; ValidateRecords ties available and withdrawals to this denom.
		if a.Balance.Denom != appconsts.BondDenom {
			return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("escrow_accounts[%d].balance", i), Want: appconsts.BondDenom, Got: a.Balance.Denom}
		}
		k := [2]string{owner, a.Balance.Denom}
		e, ok := byKey[k]
		if !ok {
			e = &FibreEscrow{Owner: sdk.AccAddress(owner).String(), Denom: k[1], Balance: math.ZeroInt(), Available: math.ZeroInt(), Pending: math.ZeroInt()}
			byKey[k] = e
		}
		if e.Balance, err = e.Balance.SafeAdd(a.Balance.Amount); err == nil {
			e.Available, err = e.Available.SafeAdd(a.AvailableBalance.Amount)
		}
		if err == nil {
			err = sum.add(a.Balance.Denom, a.Balance.Amount)
		}
		if err != nil {
			return nil, nil, overflowError(module, "escrow_accounts", err)
		}
	}
	for i, w := range gs.Withdrawals {
		owner, err := decodeAddr(accCodec, nil, module, "withdrawals", i, "signer", w.Signer)
		if err != nil {
			return nil, nil, err
		}
		if err := notAfter(module, fmt.Sprintf("withdrawals[%d].requested_timestamp", i), w.RequestedTimestamp, blockTime); err != nil {
			return nil, nil, err
		}
		e, ok := byKey[[2]string{owner, w.Amount.Denom}]
		if !ok {
			return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("withdrawals[%d]", i), Want: "an escrow of its denom", Got: w.Signer}
		}
		if e.Pending, err = e.Pending.SafeAdd(w.Amount.Amount); err != nil {
			return nil, nil, overflowError(module, "withdrawals", err)
		}
	}
	escrows := make([]FibreEscrow, 0, len(byKey))
	for _, e := range byKey {
		escrows = append(escrows, *e)
	}
	return escrows, sum, nil
}

// transferEscrows checks the channel params and returns total_escrowed and the escrow account of each transfer channel, as address -> port/channel.
func transferEscrows(cdc codec.JSONCodec, g *Genesis) (sdk.Coins, map[string]string, error) {
	const module = ibctransfertypes.ModuleName
	var transfer ibctransfertypes.GenesisState
	if err := decodeModule(cdc, g, module, &transfer); err != nil {
		return nil, nil, err
	}
	if transfer.PortId != ibctransfertypes.PortID {
		return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: "port_id", Want: ibctransfertypes.PortID, Got: transfer.PortId}
	}
	if err := validCoins(transfer.TotalEscrowed); err != nil {
		return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: "total_escrowed", Want: "valid positive coins", Got: err.Error()}
	}
	var ibc ibccoretypes.GenesisState
	if err := decodeModule(cdc, g, ibcexported.ModuleName, &ibc); err != nil {
		return nil, nil, err
	}
	// Channel InitGenesis panics on invalid params; ValidateGenesis does not check them.
	if err := ibc.ChannelGenesis.Params.Validate(); err != nil {
		return nil, nil, &CheckError{Step: stepRecords, Module: ibcexported.ModuleName, Field: "channel_genesis.params", Want: "valid params", Got: err.Error()}
	}
	// Closed channels keep their escrow.
	escrows := make(map[string]string)
	for _, ch := range ibc.ChannelGenesis.Channels {
		if ch.PortId == ibctransfertypes.PortID {
			escrows[string(ibctransfertypes.GetEscrowAddress(ch.PortId, ch.ChannelId))] = ch.PortId + "/" + ch.ChannelId
		}
	}
	return transfer.TotalEscrowed, escrows, nil
}

// checkTransfer moves the channel escrow balances out of bals, requires them to cover total_escrowed and returns them with the surplus.
func checkTransfer(escrowed sdk.Coins, escrows map[string]string, bals map[string]sdk.Coins) ([]TransferEscrow, sdk.Coins, error) {
	held := intSum{}
	out := make([]TransferEscrow, 0, len(escrows))
	for _, name := range slices.Sorted(maps.Values(escrows)) {
		if err := held.addCoins(bals[name]); err != nil {
			return nil, nil, overflowError(ibctransfertypes.ModuleName, "escrow_balances", err)
		}
		port, channel, _ := strings.Cut(name, "/")
		out = append(out, TransferEscrow{Port: port, Channel: channel, Balance: bals[name]})
		delete(bals, name)
	}
	surplus, err := coversCoins(ibctransfertypes.ModuleName, "escrow_balances", escrowed, held.coins())
	return out, surplus, err
}

// checkWarp applies the token and router identity rules, requires bal(warp) to equal the collateral of COLLATERAL tokens per origin denom, and returns the records.
func checkWarp(cdc codec.JSONCodec, g *Genesis, bal sdk.Coins) ([]warptypes.HypToken, []warptypes.GenesisRemoteRouterWrapper, error) {
	const module = warptypes.ModuleName
	var gs warptypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return nil, nil, err
	}
	ids, collateral, err := sumWarpTokens(gs.Tokens)
	if err != nil {
		return nil, nil, err
	}
	routers := make(map[[2]uint64]int, len(gs.RemoteRouters))
	for i, r := range gs.RemoteRouters {
		if _, ok := ids[r.TokenId]; !ok {
			return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: fmt.Sprintf("remote_routers[%d].token_id", i), Want: "an existing token", Got: fmt.Sprint(r.TokenId)}
		}
		key := [2]uint64{r.TokenId, uint64(r.RemoteRouter.ReceiverDomain)}
		if j, ok := routers[key]; ok {
			return nil, nil, duplicateError(module, "remote_routers", i, j, fmt.Sprintf("token %d, domain %d", r.TokenId, r.RemoteRouter.ReceiverDomain))
		}
		routers[key] = i
	}
	if err := equalCoins(module, "balance", collateral.coins(), bal); err != nil {
		return nil, nil, err
	}
	slices.SortFunc(gs.Tokens, func(a, b warptypes.HypToken) int { return cmp.Compare(a.Id.GetInternalId(), b.Id.GetInternalId()) })
	slices.SortFunc(gs.RemoteRouters, func(a, b warptypes.GenesisRemoteRouterWrapper) int {
		return cmp.Or(cmp.Compare(a.TokenId, b.TokenId), cmp.Compare(a.RemoteRouter.ReceiverDomain, b.RemoteRouter.ReceiverDomain))
	})
	return gs.Tokens, gs.RemoteRouters, nil
}

// sumWarpTokens applies the token rules and returns the index of each internal id and the collateral of COLLATERAL tokens per origin denom.
func sumWarpTokens(tokens []warptypes.HypToken) (map[uint64]int, intSum, error) {
	const module = warptypes.ModuleName
	// Import stores tokens by internal id, so two ids that share it overwrite each other.
	ids := make(map[uint64]int, len(tokens))
	collateral := intSum{}
	for i, t := range tokens {
		field := fmt.Sprintf("tokens[%d]", i)
		id := t.Id.GetInternalId()
		if j, ok := ids[id]; ok {
			return nil, nil, duplicateError(module, "tokens", i, j, t.Id.String())
		}
		ids[id] = i
		if err := nonNegative(module, field+".collateral_balance", t.CollateralBalance); err != nil {
			return nil, nil, err
		}
		// Transfers build coins of origin_denom and panic on an invalid one.
		if err := sdk.ValidateDenom(t.OriginDenom); err != nil {
			return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: field + ".origin_denom", Want: "a valid denom", Got: err.Error()}
		}
		switch t.TokenType {
		case warptypes.HYP_TOKEN_TYPE_COLLATERAL:
			if err := collateral.add(t.OriginDenom, t.CollateralBalance); err != nil {
				return nil, nil, overflowError(module, "tokens", err)
			}
		case warptypes.HYP_TOKEN_TYPE_SYNTHETIC:
		default:
			return nil, nil, &CheckError{Step: stepRecords, Module: module, Field: field + ".token_type", Want: "collateral or synthetic", Got: t.TokenType.String()}
		}
	}
	return ids, collateral, nil
}

// notAfter is the T sanity bound: a time recorded in state cannot be after the export block time.
func notAfter(module, field string, got, blockTime time.Time) error {
	if got.After(blockTime) {
		return &CheckError{Step: stepConservation, Module: module, Field: field, Want: "<= export-block-time " + formatTime(blockTime), Got: formatTime(got)}
	}
	return nil
}

// coversCoins fails on the first denom of need that have holds less of, and returns have - need.
func coversCoins(module, field string, need, have sdk.Coins) (sdk.Coins, error) {
	for _, c := range need {
		if h := have.AmountOfNoDenomValidation(c.Denom); h.LT(c.Amount) {
			return nil, &CheckError{Step: stepConservation, Module: module, Field: field + "[" + c.Denom + "]", Want: ">= " + c.Amount.String(), Got: h.String()}
		}
	}
	surplus := sdk.Coins{}
	for _, c := range have {
		if s := c.Amount.Sub(need.AmountOfNoDenomValidation(c.Denom)); s.IsPositive() {
			surplus = append(surplus, sdk.Coin{Denom: c.Denom, Amount: s})
		}
	}
	return surplus, nil
}
