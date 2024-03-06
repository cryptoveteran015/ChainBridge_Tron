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

package keystore

import (
	"bytes"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/proto/core"
)


type keystoreWallet struct {
	account  Account 
	keystore *KeyStore 
}

func (w *keystoreWallet) URL() URL {
	return w.account.URL
}

func (w *keystoreWallet) Status() (string, error) {
	w.keystore.mu.RLock()
	defer w.keystore.mu.RUnlock()

	if _, ok := w.keystore.unlocked[w.account.Address.String()]; ok {
		return "Unlocked", nil
	}
	return "Locked", nil
}

func (w *keystoreWallet) Open(passphrase string) error { return nil }
func (w *keystoreWallet) Close() error { return nil }
func (w *keystoreWallet) Accounts() []Account {
	return []Account{w.account}
}

func (w *keystoreWallet) Contains(account Account) bool {
	return bytes.Equal(account.Address, w.account.Address) && (account.URL == (URL{}) || account.URL == w.account.URL)
}

func (w *keystoreWallet) Derive(path DerivationPath, pin bool) (Account, error) {
	return Account{}, ErrNotSupported
}

func (w *keystoreWallet) signHash(acc Account, hash []byte) ([]byte, error) {
	if !w.Contains(acc) {
		return nil, ErrUnknownAccount
	}
	return w.keystore.SignHash(acc, hash)
}

func (w *keystoreWallet) SignData(acc Account, mimeType string, data []byte) ([]byte, error) {
	return w.signHash(acc, crypto.Keccak256(data))
}

func (w *keystoreWallet) SignDataWithPassphrase(acc Account, passphrase, mimeType string, data []byte) ([]byte, error) {
	if !w.Contains(acc) {
		return nil, ErrUnknownAccount
	}
	return w.keystore.SignHashWithPassphrase(acc, passphrase, crypto.Keccak256(data))
}

func (w *keystoreWallet) SignText(acc Account, text []byte, useFixedLength ...bool) ([]byte, error) {
	return w.signHash(acc, TextHash(text, useFixedLength...))
}

func (w *keystoreWallet) SignTextWithPassphrase(acc Account, passphrase string, text []byte) ([]byte, error) {
	if !w.Contains(acc) {
		return nil, ErrUnknownAccount
	}
	return w.keystore.SignHashWithPassphrase(acc, passphrase, TextHash(text))
}

func (w *keystoreWallet) SignTx(acc Account, tx *core.Transaction) (*core.Transaction, error) {
	if !w.Contains(acc) {
		return nil, ErrUnknownAccount
	}
	return w.keystore.SignTx(acc, tx)
}

func (w *keystoreWallet) SignTxWithPassphrase(acc Account, passphrase string, tx *core.Transaction) (*core.Transaction, error) {
	if !w.Contains(acc) {
		return nil, ErrUnknownAccount
	}
	return w.keystore.SignTxWithPassphrase(acc, passphrase, tx)
}
