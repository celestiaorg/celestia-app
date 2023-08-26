# AnteHandler

Celestia makes use of a Cosmos SDK [AnteHandler](https://docs.cosmos.network/v0.46/modules/auth/03_antehandlers.html#antehandlers) in order to reject decodable sdk.Txs that do not meet certain criteria. The ante handler is defined in [app/ante/ante.go](https://github.com/celestiaorg/celestia-app/blob/7f97788a64af7fe0fce00959753d6dd81663e98f/app/ante/ante.go). It chains together several decorators to ensure the following criteria are met:

- The tx does not contain any [extension options](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L119-L122).
- The tx passes `ValidateBasic()`.
- The tx's [timeout_height](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L115-L117) has not been reached if one is specified.
- The tx's [memo](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L110-L113) is <= the max memo characters. Celestia uses the default value for [`MaxMemoCharacters: 256`](<https://github.com/cosmos/cosmos-sdk/blob/a429238fc267da88a8548bfebe0ba7fb28b82a13/x/auth/README.md?plain=1#L230>).
- The tx's [gas_limit](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L211-L213) is > the gas consumed based on the tx's size. Celestia uses the default value for [`TxSizeCostPerByte: 10`](https://github.com/cosmos/cosmos-sdk/blob/a429238fc267da88a8548bfebe0ba7fb28b82a13/x/auth/README.md?plain=1#L232)
