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

	appMessageBz, err := json.MarshalIndent(appMessage, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling genesis state: %w", err)
	}

	cp := coretypes.ConsensusParamsFromProto(*params)

	genesisDoc := &coretypes.GenesisDoc{
		ChainID:         chainID,
		AppState:        appMessageBz,
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

const v3Genesis = `{
  "app_message": {
    "auth": {
      "accounts": [],
      "params": {
        "max_memo_characters": "256",
        "sig_verify_cost_ed25519": "590",
        "sig_verify_cost_secp256k1": "1000",
        "tx_sig_limit": "7",
        "tx_size_cost_per_byte": "10"
      }
    },
    "authz": {
      "authorization": []
    },
    "bank": {
      "balances": [],
      "denom_metadata": [
        {
          "base": "utia",
          "denom_units": [
            {
              "aliases": [
                "microtia"
              ],
              "denom": "utia",
              "exponent": 0
            },
            {
              "aliases": [],
              "denom": "TIA",
              "exponent": 6
            }
          ],
          "description": "The native token of the Celestia network.",
          "display": "TIA",
          "name": "TIA",
          "symbol": "TIA",
          "uri": "",
          "uri_hash": ""
        }
      ],
      "params": {
        "default_send_enabled": true,
        "send_enabled": []
      },
      "supply": []
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
        "amount": "1000",
        "denom": "utia"
      }
    },
    "distribution": {
      "delegator_starting_infos": [],
      "delegator_withdraw_infos": [],
      "fee_pool": {
        "community_pool": []
      },
      "outstanding_rewards": [],
      "params": {
        "base_proposer_reward": "0.000000000000000000",
        "bonus_proposer_reward": "0.000000000000000000",
        "community_tax": "0.020000000000000000",
        "withdraw_addr_enabled": true
      },
      "previous_proposer": "",
      "validator_accumulated_commissions": [],
      "validator_current_rewards": [],
      "validator_historical_rewards": [],
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
      "deposit_params": {
        "max_deposit_period": "604800s",
        "min_deposit": [
          {
            "amount": "10000000000",
            "denom": "utia"
          }
        ]
      },
      "deposits": [],
      "proposals": [],
      "starting_proposal_id": "1",
      "tally_params": {
        "quorum": "0.334000000000000000",
        "threshold": "0.500000000000000000",
        "veto_threshold": "0.334000000000000000"
      },
      "votes": [],
      "voting_params": {
        "voting_period": "604800s"
      }
    },
    "ibc": {
      "channel_genesis": {
        "ack_sequences": [],
        "acknowledgements": [],
        "channels": [],
        "commitments": [],
        "next_channel_sequence": "0",
        "receipts": [],
        "recv_sequences": [],
        "send_sequences": []
      },
      "client_genesis": {
        "clients": [],
        "clients_consensus": [],
        "clients_metadata": [],
        "create_localhost": false,
        "next_client_sequence": "0",
        "params": {
          "allowed_clients": [
            "06-solomachine",
            "07-tendermint"
          ]
        }
      },
      "connection_genesis": {
        "client_connection_paths": [],
        "connections": [],
        "next_connection_sequence": "0",
        "params": {
          "max_expected_time_per_block": "75000000000"
        }
      }
    },
    "interchainaccounts": {
      "controller_genesis_state": {
        "active_channels": [],
        "interchain_accounts": [],
        "params": {
          "controller_enabled": false
        },
        "ports": []
      },
      "host_genesis_state": {
        "active_channels": [],
        "interchain_accounts": [],
        "params": {
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
          ],
          "host_enabled": true
        },
        "port": "icahost"
      }
    },
    "minfee": {
      "network_min_gas_price": "0.000001000000000000"
    },
    "mint": {
      "bond_denom": "utia"
    },
    "packetfowardmiddleware": {
      "in_flight_packets": {},
      "params": {
        "fee_percentage": "0.000000000000000000"
      }
    },
    "params": null,
    "qgb": {
      "params": {
        "data_commitment_window": "400"
      }
    },
    "signal": {},
    "slashing": {
      "missed_blocks": [],
      "params": {
        "downtime_jail_duration": "60s",
        "min_signed_per_window": "0.750000000000000000",
        "signed_blocks_window": "5000",
        "slash_fraction_double_sign": "0.020000000000000000",
        "slash_fraction_downtime": "0.000000000000000000"
      },
      "signing_infos": []
    },
    "staking": {
      "delegations": [],
      "exported": false,
      "last_total_power": "0",
      "last_validator_powers": [],
      "params": {
        "bond_denom": "utia",
        "historical_entries": 10000,
        "max_entries": 7,
        "max_validators": 100,
        "min_commission_rate": "0.050000000000000000",
        "unbonding_time": "1814400s"
      },
      "redelegations": [],
      "unbonding_delegations": [],
      "validators": []
    },
    "transfer": {
      "denom_traces": [],
      "params": {
        "receive_enabled": true,
        "send_enabled": true
      },
      "port_id": "transfer"
    },
    "vesting": {}
  },
  "chain_id": "celestia-testnet",
  "gentxs_dir": "",
  "moniker": "validator1",
  "node_id": "08d457a60ce05665e12015888a0abd36ff45cfb8"
}
`
