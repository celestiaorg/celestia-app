package internal

import (
	"fmt"
	"os"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
)

func ValidateTransaction(pathToTransaction string) error {
	_, err := getTxBytes(pathToTransaction)
	if err != nil {
		return err
	}
	return nil
}

func getTxBytes(pathToTransaction string) ([]byte, error) {
	signedTx, err := os.ReadFile(pathToTransaction)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %v. %v", pathToTransaction, err)
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoded, err := encCfg.TxConfig.TxJSONDecoder()(signedTx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %v", err)
	}

	txBytes, err := encCfg.TxConfig.TxEncoder()(decoded)
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %v", err)
	}
	return txBytes, nil
}
