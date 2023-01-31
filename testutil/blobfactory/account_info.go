package blobfactory

import (
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

type AccountInfo struct {
	AccountNum, Sequence uint64
}

func ExtractAccountInfo(accs []authtypes.AccountI) []AccountInfo {
	infos := make([]AccountInfo, len(accs))
	for i, acc := range accs {
		infos[i] = AccountInfo{Sequence: acc.GetSequence(), AccountNum: acc.GetAccountNumber()}
	}
	return infos
}
