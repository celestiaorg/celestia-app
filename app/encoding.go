package app

import "github.com/tendermint/spm/cosmoscmd"

func MakeEncodingConfig() cosmoscmd.EncodingConfig {
	return cosmoscmd.MakeEncodingConfig(ModuleBasics)
}
