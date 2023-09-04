# Celestia Governance Params

**NOTE**: Referenced from Notion page [here](https://www.notion.so/celestiaorg/Gov-All-Params-7c8087a32a8c4b9c871a4585abf791f0)

Recommendation is to move this page to become
an `Active` CIP in the future that shows latest
active parameters for Celestia on mainnet.

## Parameters

| Module.Parameter | Default | Summary |
| --- | --- | --- |
| auth.MaxMemoCharacters | 256 | Largest allowed size for a memo. |
| auth.TxSigLimit | 7 | Max number of signatures (reject tx with more signs in a multisig). |
| auth.TxSizeCostPerByte | 10 | Gas used per transaction byte. |
| auth.SigVerifyCostED25519 | 590 | Gas used to verify ED25519 signature. |
| auth.SigVerifyCostSecp256k1 | 1000 | Gas used to verify secp256k1 signature. |
| bank.SendEnabled | true | Allow transfers. |
| blob.GasPerBlobByte | 8 | Gas used per blob byte. |
| MaxBlockBytes | 100MB | Hardcoded value in tendermint for the protobuf encoded block. |
| MaxSquareSize | 128 | Hardcoded value in the applications that requires a hardfork to change. |
| blob.GovMaxSquareSize | 64 | Governance parameter for the  square size. If larger than MaxSquareSize, MaxSquareSize is used. |
| consensus.block.MaxBytes | 1.8MB | Governance parameter for the maximum size of the block. |
| consensus.block.MaxGas | -1 / ∞ | Maximum gas allowed per block (-1 is infinite). |
| consensus.block.TimeIotaMs | 1000 | Minimum time added to the time in the header each block. |
| consensus.evidence.MaxAgeNumBlocks | 100000 | The maximum number of blocks before evidence is considered invalid. This value will stop CometBFT from pruning block data. |
| consensus.evidence.MaxAgeDuration | 172800000000000 | The maximum age of evidence before it is considered invalid. This value should be identical to the unbonding period. |
| consensus.evidence.MaxBytes | 1MB | Maximum size in bytes used by evidence in a given block. |
| consensus.validator.PubKeyTypes | Ed25519 | The type of public key used by validators. |
| consensus.Version.AppVersion | 1 | Determines protocol rules used for a given height. Incremented by the application upon an upgrade. |
| distribution.communitytax | 0.02 | Percentage of the inflation sent to the community pool. |
| distribution.WithdrawAddrEnabled | true | Enables delegators to withdraw funds to a different address. |
| distribution.BaseProposerReward | 0 | Reward for proposing a block. |
| distribution.BonusProposerReward | 0 | Extra reward for proposers based on the voting power included in the commit. |
| gov.DepositParams.MinDeposit |  | Minimum deposit for a proposal to enter voting period. |
| gov.DepositParams.MaxDepositPeriod |  | Maximum period for token holders to deposit on a proposal. |
| gov.VotingParams.VotingPeriod |  | Duration of the voting period. |
| gov.TallyParams.Quorum | 33.4 | Minimum percentage of total stake needed to vote for a result to be considered valid. |
| gov.TallyParams.Threshold | 50.0 | Minimum proportion of Yes votes for proposal to pass. |
| gov.TallyParams.VetoThreshold | 33.4 | Minimum value of Veto votes to Total votes ratio for proposal to be vetoed. |
| slashing.SignedBlocksWindow | 5000 | The range of blocks used to count for downtime. |
| slashing.MinSignedPerWindow | 5 | Minumum signatures in the block. |
| slashing.DowntimeJailDuration | 10 mins | Duration of time a validator must stay jailed. |
| slashing.SlashFractionDoubleSign | 1/20 | Percentage slashed after a validator is jailed for downtime. |
| slashing.SlashFractionDowntime | 1/100 | Percentage slashed after a validator is jailed for downtime. |
