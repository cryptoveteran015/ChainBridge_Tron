// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package keystore implements encrypted storage of secp256k1 private keys.
//
// Keys are stored as encrypted JSON files according to the Web3 Secret Storage specification.
// See https://github.com/ethereum/wiki/wiki/Web3-Secret-Storage-Definition for more information.
package keystore

import (
	"bytes"
	"crypto/ecdsa"
	crand "crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/proto/core"
	"google.golang.org/protobuf/proto"
)

var (
	ErrLocked  = NewAuthNeededError("password or unlock")
	ErrNoMatch = errors.New("no key for given address or file")
	ErrDecrypt = errors.New("could not decrypt key with given passphrase")

	ErrAccountAlreadyExists = errors.New("account already exists")
)

var KeyStoreType = reflect.TypeOf(&KeyStore{})

const KeyStoreScheme = "keystore"

const walletRefreshCycle = 3 * time.Second

type KeyStore struct {
	storage  keyStore   
	cache    *accountCache 
	changes  chan struct{}
	unlocked map[string]*unlocked

	wallets     []Wallet
	updateFeed  event.Feed
	updateScope event.SubscriptionScope
	updating    bool

	mu       sync.RWMutex
	importMu sync.Mutex
}

type unlocked struct {
	*Key
	abort chan struct{}
}

func NewKeyStore(keydir string, scryptN, scryptP int) *KeyStore {
	keydir, _ = filepath.Abs(keydir)
	ks := &KeyStore{storage: &keyStorePassphrase{keydir, scryptN, scryptP, false}}
	ks.init(keydir)
	return ks
}

func (ks *KeyStore) GetscryptN(str string) *unlocked {
    return ks.unlocked[str]
}

func (ks *KeyStore) init(keydir string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ks.unlocked = make(map[string]*unlocked)
	ks.cache, ks.changes = newAccountCache(keydir)

	runtime.SetFinalizer(ks, func(m *KeyStore) {
		m.cache.close()
	})
	accs := ks.cache.accounts()

	ks.wallets = make([]Wallet, len(accs))
	for i := 0; i < len(accs); i++ {
		ks.wallets[i] = &keystoreWallet{account: accs[i], keystore: ks}
	}
}

func (ks *KeyStore) Wallets() []Wallet {
	ks.refreshWallets()

	ks.mu.RLock()
	defer ks.mu.RUnlock()

	cpy := make([]Wallet, len(ks.wallets))
	copy(cpy, ks.wallets)
	return cpy
}
func (ks *KeyStore) refreshWallets() {
	ks.mu.Lock()
	accs := ks.cache.accounts()

	var (
		wallets = make([]Wallet, 0, len(accs))
		events  []WalletEvent
	)

	for _, account := range accs {
		for len(ks.wallets) > 0 && ks.wallets[0].URL().Cmp(account.URL) < 0 {
			events = append(events, WalletEvent{Wallet: ks.wallets[0], Kind: WalletDropped})
			ks.wallets = ks.wallets[1:]
		}
		if len(ks.wallets) == 0 || ks.wallets[0].URL().Cmp(account.URL) > 0 {
			wallet := &keystoreWallet{account: account, keystore: ks}

			events = append(events, WalletEvent{Wallet: wallet, Kind: WalletArrived})
			wallets = append(wallets, wallet)
			continue
		}
		if ks.wallets[0].Accounts()[0].URL == account.URL &&
			bytes.Equal(ks.wallets[0].Accounts()[0].Address, account.Address) {
			wallets = append(wallets, ks.wallets[0])
			ks.wallets = ks.wallets[1:]
			continue
		}
	}
	for _, wallet := range ks.wallets {
		events = append(events, WalletEvent{Wallet: wallet, Kind: WalletDropped})
	}
	ks.wallets = wallets
	ks.mu.Unlock()

	for _, event := range events {
		ks.updateFeed.Send(event)
	}
}

func (ks *KeyStore) Subscribe(sink chan<- WalletEvent) event.Subscription {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	sub := ks.updateScope.Track(ks.updateFeed.Subscribe(sink))

	if !ks.updating {
		ks.updating = true
		go ks.updater()
	}
	return sub
}

func (ks *KeyStore) updater() {
	for {
		select {
		case <-ks.changes:
		case <-time.After(walletRefreshCycle):
		}
		ks.refreshWallets()

		ks.mu.Lock()
		if ks.updateScope.Count() == 0 {
			ks.updating = false
			ks.mu.Unlock()
			return
		}
		ks.mu.Unlock()
	}
}

func (ks *KeyStore) HasAddress(addr address.Address) bool {
	return ks.cache.hasAddress(addr)
}

func (ks *KeyStore) Accounts() []Account {
	return ks.cache.accounts()
}

func (ks *KeyStore) Delete(a Account, passphrase string) error {
	a, key, err := ks.GetDecryptedKey(a, passphrase)
	if key != nil {
		zeroKey(key.Encode)
	}
	if err != nil {
		return err
	}
	err = os.Remove(a.URL.Path)
	if err == nil {
		ks.cache.delete(a)
		ks.refreshWallets()
	}
	return err
}

func (ks *KeyStore) SignHash(a Account, hash []byte) ([]byte, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	unlockedKey, found := ks.unlocked[a.Address.String()]
	if !found {
		return nil, ErrLocked
	}
	return crypto.Sign(hash, unlockedKey.Encode)
}

func (ks *KeyStore) SignTx(a Account, tx *core.Transaction) (*core.Transaction, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	unlockedKey, found := ks.unlocked[a.Address.String()]
	if !found {
		return nil, ErrLocked
	}

	rawData, err := proto.Marshal(tx.GetRawData())
	if err != nil {
		return nil, err
	}
	h256h := sha256.New()
	h256h.Write(rawData)
	hash := h256h.Sum(nil)

	signature, err := crypto.Sign(hash, unlockedKey.Encode)
	if err != nil {
		return nil, err
	}
	tx.Signature = append(tx.Signature, signature)
	_ = unlockedKey
	return tx, nil
}

func (ks *KeyStore) SignHashWithPassphrase(a Account, passphrase string, hash []byte) (signature []byte, err error) {
	_, key, err := ks.GetDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}
	defer zeroKey(key.Encode)
	return crypto.Sign(hash, key.Encode)
}

func (ks *KeyStore) SignTxWithPassphrase(a Account, passphrase string, tx *core.Transaction) (*core.Transaction, error) {
	_, key, err := ks.GetDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}
	defer zeroKey(key.Encode)

	rawData, err := proto.Marshal(tx.GetRawData())
	if err != nil {
		return nil, err
	}
	h256h := sha256.New()
	h256h.Write(rawData)
	hash := h256h.Sum(nil)

	signature, err := crypto.Sign(hash, key.Encode)
	if err != nil {
		return nil, err
	}
	tx.Signature = append(tx.Signature, signature)
	return tx, nil
}

func (ks *KeyStore) Unlock(a Account, passphrase string) error {
	return ks.TimedUnlock(a, passphrase, 0)
}

func (ks *KeyStore) Lock(addr address.Address) error {
	ks.mu.Lock()
	if unl, found := ks.unlocked[addr.String()]; found {
		ks.mu.Unlock()
		ks.expire(addr, unl, time.Duration(0)*time.Nanosecond)
	} else {
		ks.mu.Unlock()
	}
	return nil
}

func (ks *KeyStore) TimedUnlock(a Account, passphrase string, timeout time.Duration) error {
	a, key, err := ks.GetDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()
	u, found := ks.unlocked[a.Address.String()]
	if found {
		if u.abort == nil {
			zeroKey(key.Encode)
			return nil
		}
		close(u.abort)
	}
	if timeout > 0 {
		u = &unlocked{Key: key, abort: make(chan struct{})}
		go ks.expire(a.Address, u, timeout)
	} else {
		u = &unlocked{Key: key}
	}
	ks.unlocked[a.Address.String()] = u
	return nil
}

func (ks *KeyStore) Find(a Account) (Account, error) {
	ks.cache.maybeReload()
	ks.cache.mu.Lock()
	a, err := ks.cache.find(a)
	ks.cache.mu.Unlock()
	return a, err
}

func (ks *KeyStore) GetDecryptedKey(a Account, auth string) (Account, *Key, error) {
	a, err := ks.Find(a)
	if err != nil {
		return a, nil, err
	}
	key, err := ks.storage.GetKey(a.Address, a.URL.Path, auth)
	return a, key, err
}

func (ks *KeyStore) expire(addr address.Address, u *unlocked, timeout time.Duration) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-u.abort:
		// just quit
	case <-t.C:
		ks.mu.Lock()
		
		if ks.unlocked[addr.String()] == u {
			zeroKey(u.Encode)
			delete(ks.unlocked, addr.String())
		}
		ks.mu.Unlock()
	}
}

func (ks *KeyStore) NewAccount(passphrase string) (Account, error) {
	_, account, err := storeNewKey(ks.storage, crand.Reader, passphrase)
	if err != nil {
		return Account{}, err
	}
	
	ks.cache.add(account)
	ks.refreshWallets()
	return account, nil
}

func (ks *KeyStore) Export(a Account, passphrase, newPassphrase string) (keyJSON []byte, err error) {
	_, key, err := ks.GetDecryptedKey(a, passphrase)
	if err != nil {
		return nil, err
	}
	var N, P int
	if store, ok := ks.storage.(*keyStorePassphrase); ok {
		N, P = store.scryptN, store.scryptP
	} else {
		N, P = StandardScryptN, StandardScryptP
	}
	return EncryptKey(key, newPassphrase, N, P)
}

func (ks *KeyStore) Import(keyJSON []byte, passphrase, newPassphrase string) (Account, error) {
	key, err := DecryptKey(keyJSON, passphrase)
	if key != nil && key.Encode != nil {
		defer zeroKey(key.Encode)
	}
	if err != nil {
		return Account{}, err
	}
	ks.importMu.Lock()
	defer ks.importMu.Unlock()

	if ks.cache.hasAddress(key.Address) {
		return Account{
			Address: key.Address,
		}, ErrAccountAlreadyExists
	}
	return ks.importKey(key, newPassphrase)
}

func (ks *KeyStore) ImportECDSA(priv *ecdsa.PrivateKey, passphrase string) (Account, error) {
	ks.importMu.Lock()
	defer ks.importMu.Unlock()

	key := newKeyFromECDSA(priv)
	if ks.cache.hasAddress(key.Address) {
		return Account{
			Address: key.Address,
		}, ErrAccountAlreadyExists
	}
	return ks.importKey(key, passphrase)
}

func (ks *KeyStore) importKey(key *Key, passphrase string) (Account, error) {
	a := Account{Address: key.Address, URL: URL{Scheme: KeyStoreScheme, Path: ks.storage.JoinPath(keyFileName(key.Address))}}
	if err := ks.storage.StoreKey(a.URL.Path, key, passphrase); err != nil {
		return Account{}, err
	}
	ks.cache.add(a)
	ks.refreshWallets()
	return a, nil
}

func (ks *KeyStore) Update(a Account, passphrase, newPassphrase string) error {
	a, key, err := ks.GetDecryptedKey(a, passphrase)
	if err != nil {
		return err
	}
	return ks.storage.StoreKey(a.URL.Path, key, newPassphrase)
}

func zeroKey(k *ecdsa.PrivateKey) {
	b := k.D.Bits()
	for i := range b {
		b[i] = 0
	}
}
