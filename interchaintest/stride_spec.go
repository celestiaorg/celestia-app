package interchaintest_test

import (
	"encoding/json"
	"fmt"

	"github.com/strangelove-ventures/interchaintest/v6"
	"github.com/strangelove-ventures/interchaintest/v6/ibc"

	"github.com/icza/dyno"
)

var strideSpec = &interchaintest.ChainSpec{
	Name:          "stride",
	ChainName:     "stride",
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
	ChainConfig: ibc.ChainConfig{
		Type:    "cosmos",
		Name:    "stride",
		ChainID: "stride-1",
		Images: []ibc.DockerImage{{
			Repository: "ghcr.io/strangelove-ventures/heighliner/stride",
			// TODO: Update this to the latest version
			// See https://github.com/cosmos/relayer/issues/1432
			Version: "andrew-test_admin_v5.1.1",
			UidGid:  "1025:1025",
		}},
		Bin:            "strided",
		Bech32Prefix:   "stride",
		Denom:          "ustrd",
		GasPrices:      "0.0ustrd",
		TrustingPeriod: TrustingPeriod,
		GasAdjustment:  1.1,
		ModifyGenesis:  ModifyGenesisStride(),
		// EncodingConfig: StrideEncoding(),
	},
}

const (
	StrideAdminAccount  = "admin"
	StrideAdminMnemonic = "tone cause tribe this switch near host damage idle fragile antique tail soda alien depth write wool they rapid unfold body scan pledge soft"
)

const (
	DayEpochIndex    = 1
	DayEpochLen      = "100s"
	StrideEpochIndex = 2
	StrideEpochLen   = "40s"
	IntervalLen      = 1
	VotingPeriod     = "30s"
	MaxDepositPeriod = "30s"
	UnbondingTime    = "200s"
	TrustingPeriod   = "199s"
)

var AllowMessages = []string{
	"/cosmos.bank.v1beta1.MsgSend",
	"/cosmos.bank.v1beta1.MsgMultiSend",
	"/cosmos.staking.v1beta1.MsgDelegate",
	"/cosmos.staking.v1beta1.MsgUndelegate",
	"/cosmos.staking.v1beta1.MsgRedeemTokensforShares",
	"/cosmos.staking.v1beta1.MsgTokenizeShares",
	"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
	"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
	"/ibc.applications.transfer.v1.MsgTransfer",
}

type HostZoneAccount struct {
	Address string `json:"address"`
	// Delegations [] `json:"delegations"`
	Target string `json:"target"`
}

type HostZoneValidator struct {
	Address              string `json:"address"`
	CommissionRate       string `json:"commissionRate"`
	DelegationAmt        string `json:"delegationAmt"`
	InternalExchangeRate string `json:"internalExchangeRate"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	Weight               string `json:"weight"`
}

type HostZoneWrapper struct {
	HostZone HostZone `json:"HostZone"`
}

type HostZone struct {
	HostDenom             string              `json:"HostDenom"`
	IBCDenom              string              `json:"IBCDenom"`
	LastRedemptionRate    string              `json:"LastRedemptionRate"`
	RedemptionRate        string              `json:"RedemptionRate"`
	Address               string              `json:"address"`
	Bech32prefix          string              `json:"bech32pref ix"`
	ChainID               string              `json:"chainId"`
	ConnectionID          string              `json:"connectionId"`
	DelegationAccount     HostZoneAccount     `json:"delegationAccount"`
	FeeAccount            HostZoneAccount     `json:"feeAccount"`
	RedemptionAccount     HostZoneAccount     `json:"redemptionAccount"`
	WithdrawalAccount     HostZoneAccount     `json:"withdrawalAccount"`
	StakedBal             string              `json:"stakedBal"`
	TransferChannelId     string              `json:"transferChannelId"`
	UnbondingFrequency    string              `json:"unbondingFrequency"`
	Validators            []HostZoneValidator `json:"validators"`
	BlacklistedValidators []HostZoneValidator `json:"blacklistedValidators"`
}

type DepositRecord struct {
	Id                 string `json:"id,omitempty"`
	Amount             string `json:"amount,omitempty"`
	Denom              string `json:"denom,omitempty"`
	HostZoneId         string `json:"hostZoneId,omitempty"`
	Status             string `json:"status,omitempty"`
	DepositEpochNumber string `json:"depositEpochNumber,omitempty"`
	Source             string `json:"source,omitempty"`
}

type DepositRecordWrapper struct {
	DepositRecord []DepositRecord `json:"DepositRecord"`
}

type UserRedemptionRecordWrapper struct {
	UserRedemptionRecord []UserRedemptionRecord `json:"UserRedemptionRecord"`
}

type UserRedemptionRecord struct {
	ID             string `json:"id"`
	Sender         string `json:"sender"`
	Receiver       string `json:"receiver"`
	Amount         string `json:"amount"`
	Denom          string `json:"denom"`
	HostZoneID     string `json:"hostZoneId"`
	EpochNumber    string `json:"epochNumber"`
	ClaimIsPending bool   `json:"claimIsPending"`
}

// func StrideEncoding() *simappparams.EncodingConfig {
// 	cfg := cosmos.DefaultEncoding()
// 	return &cfg
// }

func ModifyGenesisStride() func(ibc.ChainConfig, []byte) ([]byte, error) {
	return func(cfg ibc.ChainConfig, genbz []byte) ([]byte, error) {
		g := make(map[string]interface{})
		if err := json.Unmarshal(genbz, &g); err != nil {
			return nil, fmt.Errorf("failed to unmarshal genesis file: %w", err)
		}

		if err := dyno.Set(g, DayEpochLen, "app_state", "epochs", "epochs", DayEpochIndex, "duration"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, StrideEpochLen, "app_state", "epochs", "epochs", StrideEpochIndex, "duration"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, UnbondingTime, "app_state", "staking", "params", "unbonding_time"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, IntervalLen, "app_state", "stakeibc", "params", "rewards_interval"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, IntervalLen, "app_state", "stakeibc", "params", "delegate_interval"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, IntervalLen, "app_state", "stakeibc", "params", "deposit_interval"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, IntervalLen, "app_state", "stakeibc", "params", "redemption_rate_interval"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, IntervalLen, "app_state", "stakeibc", "params", "reinvest_interval"); err != nil {
			return nil, err
		}
		if err := dyno.Set(g, VotingPeriod, "app_state", "gov", "voting_params", "voting_period"); err != nil {
			return nil, fmt.Errorf("failed to set voting period in genesis json: %w", err)
		}
		if err := dyno.Set(g, MaxDepositPeriod, "app_state", "gov", "deposit_params", "max_deposit_period"); err != nil {
			return nil, fmt.Errorf("failed to set voting period in genesis json: %w", err)
		}

		out, err := json.Marshal(g)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal genesis bytes to json: %w", err)
		}
		return out, nil
	}
}

func ModifyGenesisStrideCounterparty() func(ibc.ChainConfig, []byte) ([]byte, error) {
	return func(cfg ibc.ChainConfig, genbz []byte) ([]byte, error) {
		g := make(map[string]interface{})
		if err := json.Unmarshal(genbz, &g); err != nil {
			return nil, fmt.Errorf("failed to unmarshal genesis file: %w", err)
		}

		if err := dyno.Set(g, UnbondingTime,
			"app_state", "staking", "params", "unbonding_time",
		); err != nil {
			return nil, err
		}

		if err := dyno.Set(g, AllowMessages,
			"app_state", "interchainaccounts", "host_genesis_state", "params", "allow_messages",
		); err != nil {
			return nil, err
		}

		out, err := json.Marshal(g)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal genesis bytes to json: %w", err)
		}
		return out, nil
	}
}
