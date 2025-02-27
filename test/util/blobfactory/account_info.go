package blobfactory

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type AccountInfo struct {
	AccountNum, Sequence uint64
}

func ExtractAccountInfo(accs []sdk.AccountI) []AccountInfo {
	infos := make([]AccountInfo, len(accs))
	for i, acc := range accs {
		infos[i] = AccountInfo{Sequence: acc.GetSequence(), AccountNum: acc.GetAccountNumber()}
	}
	return infos
}
