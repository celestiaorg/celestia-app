package keystore

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rs/zerolog"
)

type PersonalSignFn func(account common.Address, data []byte) (sig []byte, err error)

type SignerFn = bind.SignerFn

type EthKeyStore interface {
	PrivateKey(account common.Address, password string) (*ecdsa.PrivateKey, error)
	SignerFn(chainID uint64, account common.Address, password string) (SignerFn, error)
	PersonalSignFn(account common.Address, password string) (PersonalSignFn, error)
	UnsetKey(account common.Address, password string)
	Accounts() []common.Address
	AddPath(keystorePath string) error
	RemovePath(keystorePath string)
	Paths() []string
}

func New(logger zerolog.Logger, paths ...string) (EthKeyStore, error) {
	ks := &keyStore{
		logger:   logger.With().Str("module", "eth_key_store").Logger(),
		cache:    NewKeyCache(),
		paths:    make(map[string]struct{}),
		pathsMux: new(sync.RWMutex),
	}

	for _, path := range paths {
		ks.paths[path] = struct{}{}
	}
	ks.reloadPathsCache()

	return ks, nil
}

type keyStore struct {
	logger   zerolog.Logger
	cache    KeyCache
	paths    map[string]struct{}
	pathsMux *sync.RWMutex
}

func (ks *keyStore) PrivateKey(account common.Address, password string) (*ecdsa.PrivateKey, error) {
	return ks.cache.PrivateKey(account, password)
}

func (ks *keyStore) SignerFn(chainID uint64, account common.Address, password string) (SignerFn, error) {
	return ks.cache.SignerFn(chainID, account, password)
}

func (ks *keyStore) PersonalSignFn(account common.Address, password string) (PersonalSignFn, error) {
	return ks.cache.PersonalSignFn(account, password)
}

func (ks *keyStore) UnsetKey(account common.Address, password string) {
	ks.cache.UnsetKey(account, password)
}

func (ks *keyStore) Accounts() []common.Address {
	paths := ks.Paths()

	var accounts []common.Address
	for _, keystorePath := range paths {
		if err := ks.forEachWallet(keystorePath, func(spec *WalletSpec) error {
			accounts = append(accounts, spec.AddressFromHex())
			return nil
		}); err != nil {
			ks.logger.Err(err).
				Str("keystore_path", keystorePath).
				Msg("failed to read keystore files")
		}
	}

	return accounts
}

func (ks *keyStore) forEachWallet(keystorePath string, fn func(spec *WalletSpec) error) error {
	return filepath.Walk(keystorePath, func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case path == keystorePath:
			return nil
		case info.IsDir():
			return filepath.SkipDir
		}
		// Original
		// if err != nil {
		// 	return err
		// } else if path == keystorePath {
		// 	return nil
		// } else if info.IsDir() {
		// 	return filepath.SkipDir
		// }
		var spec *WalletSpec
		if data, err := ioutil.ReadFile(path); err != nil {
			return err
		} else if err = json.Unmarshal(data, &spec); err != nil {
			return err
		}
		if len(spec.Address) == 0 {
			return fmt.Errorf("failed to load address from %s", path)
		} else if !common.IsHexAddress(spec.Address) {
			return fmt.Errorf("wrong (not hex) address from %s", path)
		}
		spec.Path = path
		return fn(spec)
	})
}

func (ks *keyStore) AddPath(keystorePath string) error {
	f, err := os.Stat(keystorePath)
	if err != nil {
		return err
	} else if !f.IsDir() {
		return fmt.Errorf("%s is not a directory", keystorePath)
	}

	ks.pathsMux.Lock()
	ks.paths[keystorePath] = struct{}{}
	ks.pathsMux.Unlock()

	ks.reloadPathsCache()

	return nil
}

func (ks *keyStore) reloadPathsCache() {
	paths := ks.Paths()
	for _, keystorePath := range paths {
		err := ks.forEachWallet(keystorePath, func(spec *WalletSpec) error {
			_ = ks.cache.SetPath(spec.AddressFromHex(), spec.Path)
			return nil
		})
		if err != nil {
			ks.logger.Err(err).
				Str("keystore_path", keystorePath).
				Msg("failed to read keystore files")
		}
	}
}

func (ks *keyStore) RemovePath(keystorePath string) {
	ks.pathsMux.Lock()
	delete(ks.paths, keystorePath)
	ks.pathsMux.Unlock()
}

func (ks *keyStore) Paths() []string {
	ks.pathsMux.RLock()
	paths := make([]string, 0, len(ks.paths))
	for p := range ks.paths {
		paths = append(paths, p)
	}
	ks.pathsMux.RUnlock()
	sort.Strings(paths)
	return paths
}

type WalletSpec struct {
	Address string `json:"address"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Path    string `json:"-"`
}

func (spec *WalletSpec) AddressFromHex() common.Address {
	return common.HexToAddress(spec.Address)
}

func PrivateKeyPersonalSignFn(privKey *ecdsa.PrivateKey) (PersonalSignFn, error) {
	keyAddress := crypto.PubkeyToAddress(privKey.PublicKey)

	signFn := func(from common.Address, data []byte) (sig []byte, err error) {
		if from != keyAddress {
			return nil, errors.New("from address mismatch")
		}

		protectedHash := accounts.TextHash(data)
		return crypto.Sign(protectedHash, privKey)
	}

	return signFn, nil
}
