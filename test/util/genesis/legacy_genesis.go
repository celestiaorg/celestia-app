package genesis

import (
	"encoding/json"
	"fmt"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"time"
)

// DocumentLegacy will create a valid genesis doc with funded addresses.
func DocumentLegacy(
	defaultGenesis map[string]json.RawMessage,
	ecfg encoding.Config,
	params *tmproto.ConsensusParams,
	chainID string,
	gentxs []json.RawMessage,
	accounts []Account,
	genesisTime time.Time,
) (*coretypes.GenesisDoc, error) {

	genutilGenState := genutiltypes.DefaultGenesisState()
	genutilGenState.GenTxs = gentxs

	genBals, genAccs, err := accountsToSDKTypes(accounts)
	if err != nil {
		return nil, fmt.Errorf("converting accounts into sdk types: %w", err)
	}

	sdkAccounts, err := authtypes.PackAccounts(genAccs)
	if err != nil {
		return nil, fmt.Errorf("packing accounts: %w", err)
	}

	authGenState := authtypes.DefaultGenesisState()
	authGenState.Accounts = append(authGenState.Accounts, sdkAccounts...)

	state := defaultGenesis
	state[authtypes.ModuleName] = ecfg.Codec.MustMarshalJSON(authGenState)
	state[banktypes.ModuleName] = getLegacyBankState(genBals)
	state[genutiltypes.ModuleName] = ecfg.Codec.MustMarshalJSON(genutilGenState)

	appStateBz, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling genesis state: %w", err)
	}

	cp := coretypes.ConsensusParamsFromProto(*params)

	genesisDoc := &coretypes.GenesisDoc{
		ChainID:         chainID,
		AppState:        appStateBz,
		ConsensusParams: &cp,
		GenesisTime:     genesisTime,
	}

	return genesisDoc, nil
}

func getLegacyBankState(genBals []banktypes.Balance) []byte {
	bankGenState := banktypes.DefaultGenesisState()
	bankGenState.Balances = append(bankGenState.Balances, genBals...)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	bankGenState.Params.SendEnabled = make([]*banktypes.SendEnabled, 0)
	for _, se := range bankGenState.SendEnabled {
		bankGenState.Params.SendEnabled = append(bankGenState.Params.SendEnabled, &se)
	}
	bankGenState.SendEnabled = nil

	bz, err := json.Marshal(bankGenState)
	if err != nil {
		panic(err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(bz, &jsonMap); err != nil {
		panic(err)
	}

	delete(jsonMap, "send_enabled") // send_enabled does not have omitempty

	bz, err = json.Marshal(jsonMap)
	if err != nil {
		panic(err)
	}
	return bz
}

const v3GenesisAppState = `{
  "auth": {
    "params": {
      "max_memo_characters": "256",
      "tx_sig_limit": "7",
      "tx_size_cost_per_byte": "10",
      "sig_verify_cost_ed25519": "590",
      "sig_verify_cost_secp256k1": "1000"
    },
    "accounts": []
  },
  "authz": {
    "authorization": []
  },
  "bank": {
    "params": {
      "send_enabled": [],
      "default_send_enabled": true
    },
    "balances": [],
    "supply": [],
    "denom_metadata": [
      {
        "description": "The native token of the Celestia network.",
        "denom_units": [
          {
            "denom": "utia",
            "exponent": 0,
            "aliases": [
              "microtia"
            ]
          },
          {
            "denom": "TIA",
            "exponent": 6,
            "aliases": []
          }
        ],
        "base": "utia",
        "display": "TIA",
        "name": "TIA",
        "symbol": "TIA",
        "uri": "",
        "uri_hash": ""
      }
    ]
  },
  "blob": {
    "params": {
      "gas_per_blob_byte": 8,
      "gov_max_square_size": "64"
    }
  },
  "capability": {
    "index": "1",
    "owners": []
  },
  "crisis": {
    "constant_fee": {
      "denom": "utia",
      "amount": "1000"
    }
  },
  "distribution": {
    "params": {
      "community_tax": "0.020000000000000000",
      "base_proposer_reward": "0.000000000000000000",
      "bonus_proposer_reward": "0.000000000000000000",
      "withdraw_addr_enabled": true
    },
    "fee_pool": {
      "community_pool": []
    },
    "delegator_withdraw_infos": [],
    "previous_proposer": "",
    "outstanding_rewards": [],
    "validator_accumulated_commissions": [],
    "validator_historical_rewards": [],
    "validator_current_rewards": [],
    "delegator_starting_infos": [],
    "validator_slash_events": []
  },
  "evidence": {
    "evidence": []
  },
  "feegrant": {
    "allowances": []
  },
  "genutil": {
    "gen_txs": []
  },
  "gov": {
    "starting_proposal_id": "1",
    "deposits": [],
    "votes": [],
    "proposals": [],
    "deposit_params": {
      "min_deposit": [
        {
          "denom": "utia",
          "amount": "10000000000"
        }
      ],
      "max_deposit_period": "604800s"
    },
    "voting_params": {
      "voting_period": "604800s"
    },
    "tally_params": {
      "quorum": "0.334000000000000000",
      "threshold": "0.500000000000000000",
      "veto_threshold": "0.334000000000000000"
    }
  },
  "ibc": {
    "client_genesis": {
      "clients": [],
      "clients_consensus": [],
      "clients_metadata": [],
      "params": {
        "allowed_clients": [
          "06-solomachine",
          "07-tendermint"
        ]
      },
      "create_localhost": false,
      "next_client_sequence": "0"
    },
    "connection_genesis": {
      "connections": [],
      "client_connection_paths": [],
      "next_connection_sequence": "0",
      "params": {
        "max_expected_time_per_block": "75000000000"
      }
    },
    "channel_genesis": {
      "channels": [],
      "acknowledgements": [],
      "commitments": [],
      "receipts": [],
      "send_sequences": [],
      "recv_sequences": [],
      "ack_sequences": [],
      "next_channel_sequence": "0"
    }
  },
  "interchainaccounts": {
    "controller_genesis_state": {
      "active_channels": [],
      "interchain_accounts": [],
      "ports": [],
      "params": {
        "controller_enabled": false
      }
    },
    "host_genesis_state": {
      "active_channels": [],
      "interchain_accounts": [],
      "port": "icahost",
      "params": {
        "host_enabled": true,
        "allow_messages": [
          "/ibc.applications.transfer.v1.MsgTransfer",
          "/cosmos.bank.v1beta1.MsgSend",
          "/cosmos.staking.v1beta1.MsgDelegate",
          "/cosmos.staking.v1beta1.MsgBeginRedelegate",
          "/cosmos.staking.v1beta1.MsgUndelegate",
          "/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation",
          "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
          "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
          "/cosmos.distribution.v1beta1.MsgFundCommunityPool",
          "/cosmos.gov.v1.MsgVote",
          "/cosmos.feegrant.v1beta1.MsgGrantAllowance",
          "/cosmos.feegrant.v1beta1.MsgRevokeAllowance"
        ]
      }
    }
  },
  "minfee": {
    "network_min_gas_price": "0.000001000000000000"
  },
  "mint": {
    "bond_denom": "utia"
  },
  "packetfowardmiddleware": {
    "params": {
      "fee_percentage": "0.000000000000000000"
    },
    "in_flight_packets": {}
  },
  "params": null,
  "qgb": {
    "params": {
      "data_commitment_window": "400"
    }
  },
  "signal": {},
  "slashing": {
    "params": {
      "signed_blocks_window": "5000",
      "min_signed_per_window": "0.750000000000000000",
      "downtime_jail_duration": "60s",
      "slash_fraction_double_sign": "0.020000000000000000",
      "slash_fraction_downtime": "0.000000000000000000"
    },
    "signing_infos": [],
    "missed_blocks": []
  },
  "staking": {
    "params": {
      "unbonding_time": "1814400s",
      "max_validators": 100,
      "max_entries": 7,
      "historical_entries": 10000,
      "bond_denom": "utia",
      "min_commission_rate": "0.050000000000000000"
    },
    "last_total_power": "0",
    "last_validator_powers": [],
    "validators": [],
    "delegations": [],
    "unbonding_delegations": [],
    "redelegations": [],
    "exported": false
  },
  "transfer": {
    "port_id": "transfer",
    "denom_traces": [],
    "params": {
      "send_enabled": true,
      "receive_enabled": true
    }
  },
  "vesting": {}
}
`
