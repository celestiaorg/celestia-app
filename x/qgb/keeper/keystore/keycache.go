package keystore

import (
	"crypto/ecdsa"
	// #nosec G505
	"crypto/sha1"
	"io/ioutil"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/pkg/errors"
)

type KeyCache interface {
	SetPath(account common.Address, path string) (existing bool)
	UnsetPath(account common.Address)
	PrivateKey(account common.Address, password string) (*ecdsa.PrivateKey, error)
	SetPrivateKey(account common.Address, pk *ecdsa.PrivateKey)
	UnsetKey(account common.Address, password string)
	SignerFn(chainID uint64, account common.Address, password string) (SignerFn, error)
	PersonalSignFn(account common.Address, password string) (PersonalSignFn, error)
}

func NewKeyCache() KeyCache {
	return &keyCache{
		paths: new(sync.Map),
		keys:  new(sync.Map),
		guard: new(sync.Map),
	}
}

type keyCache struct {
	paths *sync.Map // map[common.Address]string
	keys  *sync.Map // map[string]*ecdsa.PrivateKey
	guard *sync.Map
}

func (k *keyCache) SetPath(account common.Address, path string) (existing bool) {
	_, existing = k.paths.LoadOrStore(account, path)
	if existing {
		// overwrite
		k.paths.Store(account, path)
	}

	return
}

func (k *keyCache) UnsetPath(account common.Address) {
	k.paths.Delete(account)
}

func (k *keyCache) UnsetKey(account common.Address, password string) {
	h := hashAccountPass(account, password)
	k.keys.Delete(string(h))
}

func (k *keyCache) SetPrivateKey(account common.Address, pk *ecdsa.PrivateKey) {
	h := hashAccountPass(account, "")
	k.keys.Store(string(h), pk)
}

func (k *keyCache) PrivateKey(account common.Address, password string) (*ecdsa.PrivateKey, error) {
	h := hashAccountPass(account, password)

	mux, _ := k.guard.LoadOrStore(account, new(sync.Mutex))
	mux.(*sync.Mutex).Lock()
	defer mux.(*sync.Mutex).Unlock()

	v, ok := k.keys.Load(string(h))
	if ok {
		return v.(*ecdsa.PrivateKey), nil
	}

	v, ok = k.paths.Load(account)
	if !ok {
		err := errors.Errorf("no keystore path set for account %s", account.String())
		return nil, err
	}

	path := v.(string)

	path = strings.TrimPrefix(path, "keystore://")

	keyJSON, err := ioutil.ReadFile(path)
	if err != nil {
		err = errors.Wrap(err, "failed to load a file from keystore")
		return nil, err
	}

	pk, err := keystore.DecryptKey(keyJSON, password)
	if err != nil {
		err = errors.Wrap(err, "key decryption failed")
		return nil, err
	}

	k.keys.Store(string(h), pk.PrivateKey)
	return pk.PrivateKey, nil
}

func (k *keyCache) SignerFn(chainID uint64, account common.Address, password string) (SignerFn, error) {
	key, err := k.PrivateKey(account, password)
	if err != nil {
		return nil, err
	}

	txOpts, err := bind.NewKeyedTransactorWithChainID(key, new(big.Int).SetUint64(chainID))
	if err != nil {
		err = errors.Wrap(err, "failed to init NewKeyedTransactorWithChainID")
		return nil, err
	}

	return txOpts.Signer, nil
}

func (k *keyCache) PersonalSignFn(account common.Address, password string) (PersonalSignFn, error) {
	key, err := k.PrivateKey(account, password)
	if err != nil {
		return nil, err
	}

	keyAddress := crypto.PubkeyToAddress(key.PublicKey)
	if keyAddress != account {
		return nil, errors.New("account key address mismatch")
	}

	signFn := func(from common.Address, data []byte) (sig []byte, err error) {
		if from != keyAddress {
			return nil, errors.New("from address mismatch")
		}

		protectedHash := accounts.TextHash(data)
		return crypto.Sign(protectedHash, key)
	}

	return signFn, nil
}

var hashSep = []byte("-")

func hashAccountPass(account common.Address, password string) []byte {
	// #nosec G401
	h := sha1.New()
	h.Write(account[:])
	h.Write(hashSep)
	h.Write([]byte(password))
	return h.Sum(nil)
}
