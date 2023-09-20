# Celestia Governance Params

These are the parameters for mainnet. Note that not all of these parameters are
changable via governance. This list also includes parameter that require a
hardfork to change due to being manually hardcoded in the application or they
are blocked by the `x/paramfilter` module.

## Parameters

| Module.Parameter | Default | Summary | Changeable via Governance |
| --- | --- | --- | --- |
| auth.MaxMemoCharacters | 256 | Largest allowed size for a memo in bytes. | True |
| auth.TxSigLimit | 7 | Max number of signatures allowed in a multisig transaction. | True |
| auth.TxSizeCostPerByte | 10 | Gas used per transaction byte. | True |
| auth.SigVerifyCostED25519 | 590 | Gas used to verify Ed25519 signature. | True |
| auth.SigVerifyCostSecp256k1 | 1000 | Gas used to verify secp256k1 signature. | True |
| bank.SendEnabled | true | Allow transfers. | False |
| blob.GasPerBlobByte | 8 | Gas used per blob byte. | True |
| MaxBlockBytes | 100MiB | Hardcoded value in CometBFT for the protobuf encoded block. | False |
| MaxSquareSize | 128 | Hardcoded maximum square size determined per shares per row or column for the original data square (not yet extended). | False |
| blob.GovMaxSquareSize | 64 | Governance parameter for the maximum square size determined per shares per row or column for the original data square (not yet extended)s. If larger than MaxSquareSize, MaxSquareSize is used. | True |
| consensus.block.MaxBytes | 1.88MiB | Governance parameter for the maximum size of the protobuf encoded block. | True |
| consensus.block.MaxGas | -1 | Maximum gas allowed per block (-1 is infinite). | True |
| consensus.block.TimeIotaMs | 1000 | Minimum time added to the time in the header each block. | False |
| consensus.evidence.MaxAgeNumBlocks | 120960 | The maximum number of blocks before evidence is considered invalid. This value will stop CometBFT from pruning block data. | True |
| consensus.evidence.MaxAgeDuration | 1814400000000000 (21 days) | The maximum age of evidence before it is considered invalid in nanoseconds. This value should be identical to the unbonding period. | True |
| consensus.evidence.MaxBytes | 1MiB | Maximum size in bytes used by evidence in a given block. | True |
| consensus.validator.PubKeyTypes | Ed25519 | The type of public key used by validators. | False |
| consensus.Version.AppVersion | 1 | Determines protocol rules used for a given height. Incremented by the application upon an upgrade. | False |
| distribution.CommunityTax | 0.02 (2%) | Percentage of the inflation sent to the community pool. | True |
| distribution.WithdrawAddrEnabled | true | Enables delegators to withdraw funds to a different address. | True |
| distribution.BaseProposerReward | 0 | Reward in the mint demonination for proposing a block. | True |
| distribution.BonusProposerReward | 0 | Extra reward in the mint denomination for proposers based on the voting power included in the commit. | True |
| gov.DepositParams.MinDeposit | 1000000000utia (1000 TIA) | Minimum deposit for a proposal to enter voting period. | True |
| gov.DepositParams.MaxDepositPeriod | 1209600000000000 (1 week) | Maximum period for token holders to deposit on a proposal in nanoseconds. | True |
| gov.VotingParams.VotingPeriod | 604800000000000 (2 weeks) | Duration of the voting period in nanoseconds. | True |
| gov.TallyParams.Quorum | 0.334 (33.4%) | Minimum percentage of total stake needed to vote for a result to be considered valid. | True |
| gov.TallyParams.Threshold | 0.50 (50%) | Minimum proportion of Yes votes for proposal to pass. | True |
| gov.TallyParams.VetoThreshold | 0.334 (33.4%) | Minimum value of Veto votes to Total votes ratio for proposal to be vetoed. | True |
| ibc.ClientGenesis.AllowedClients | []string{"06-solomachine", "07-tendermint"} | List of allowed IBC light clients. | True |
| ibc.ConnectionGenesis.MaxExpectedTimePerBlock | 7500000000000 (75 seconds) | Maximum expected time per block in nanoseconds under normal operation. | True |
| ibc.Transfer.SendEnabled | true | Enable sending tokens via IBC. | True |
| ibc.Transfer.ReceiveEnabled | true | Enable receiving tokens via IBC. | True |
| slashing.SignedBlocksWindow | 5000 | The range of blocks used to count for downtime. | True |
| slashing.MinSignedPerWindow | 0.75 (75%) | The percentage of SignedBlocksWindow that must be signed not to get jailed. | True |
| slashing.DowntimeJailDuration | 1 min | Duration of time a validator must stay jailed. | True |
| slashing.SlashFractionDoubleSign | 0.05 (5%) | Percentage slashed after a validator is jailed for double signing. | True |
| slashing.SlashFractionDowntime | 0.00 (0%) | Percentage slashed after a validator is jailed for downtime. | True |
| staking.UnbondingTime | 1814400 (21 days) | Duration of time for unbonding in seconds. | False |
| staking.MaxValidators | 100 | Maximum number of validators. | False |
| staking.MaxEntries | 7 | Maximum number of entries in the redelegation queue. | True |
| staking.HistoricalEntries | 10000 | Number of historical entries to persist in store. | True |
| staking.BondDenom | utia | Bondable coin denomination. | False |
| staking.MinCommissionRate | 0.05 (5%) | Minimum commission rate used by all validators. | True |
| mint.BondDenom | utia | Denomination that is inflated and sent to the distribution module account. | False |
| mint.InflationRateChange | 0.10 (10%) | The rate at which the annual provisions decrease each year. | False |
| mint.InflationRate | 0.08 (8%) | Initial annual inflation rate used to calculate the annual provisions. | False |
| qgb.DataCommitmentWindow | 400 | Number of blocks that are included in a signed batch (DataCommitment). | True |
