package store

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	c "github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/keystore"
	"github.com/pkg/errors"

	homedir "github.com/mitchellh/go-homedir"
)

func init() {
	uDir, _ := homedir.Dir()
	tronCTLDir := path.Join(uDir, common.DefaultConfigDirName, common.DefaultConfigAccountAliasesDirName)
	if _, err := os.Stat(tronCTLDir); os.IsNotExist(err) {
		os.MkdirAll(tronCTLDir, 0700)
	}
}

func LocalAccounts(keystorepath string) []string {
	files, _ := ioutil.ReadDir(path.Join(
		keystorepath,
		common.DefaultConfigAccountAliasesDirName,
	))
	accounts := []string{}

	for _, node := range files {
		if node.IsDir() {
			accounts = append(accounts, path.Base(node.Name()))
		}
	}
	return accounts
}

var (
	describe = fmt.Sprintf("%-24s\t\t%23s\n", "NAME", "ADDRESS")
	ErrNoUnlockBadPassphrase = fmt.Errorf("could not unlock account with passphrase, perhaps need different phrase")
)

func DoesNamedAccountExist(name string, keystorepath string) bool {
	for _, account := range LocalAccounts(keystorepath) {
		if account == name {
			return true
		}
	}
	return false
}

func FromAddress(addr string, keystorepath string) *keystore.KeyStore {
	for _, name := range LocalAccounts(keystorepath) {
		ks := FromAccountName(name, keystorepath)
		allAccounts := ks.Accounts()
		for _, account := range allAccounts {
			if addr == account.Address.String() {
				return ks
			}
		}
	}
	return nil
}

func FromAccountName(name string, keystorepath string) *keystore.KeyStore {
	p := path.Join(keystorepath, c.DefaultConfigAccountAliasesDirName, name)
	return keystore.ForPath(p)
}

func DefaultLocation() string {
	uDir, _ := homedir.Dir()
	return path.Join(uDir, c.DefaultConfigDirName, c.DefaultConfigAccountAliasesDirName)
}

func SetDefaultLocation(directory string) {
	c.DefaultConfigDirName = directory
	uDir, _ := homedir.Dir()
	tronCTLDir := path.Join(uDir, common.DefaultConfigDirName, common.DefaultConfigAccountAliasesDirName)
	if _, err := os.Stat(tronCTLDir); os.IsNotExist(err) {
		os.MkdirAll(tronCTLDir, 0700)
	}
}

func UnlockedKeystore(from, passphrase string, keystorepath string) (*keystore.KeyStore, *keystore.Account, error) {
	sender, err := address.Base58ToAddress(from)
	if err != nil {
		return nil, nil, fmt.Errorf("address not valid: %s", from)
	}
	ks := FromAddress(from, keystorepath)
	if ks == nil {
		return nil, nil, fmt.Errorf("could not open local keystore for %s", from)
	}
	account, lookupErr := ks.Find(keystore.Account{Address: sender})
	if lookupErr != nil {
		return nil, nil, fmt.Errorf("could not find %s in keystore", from)
	}
	if unlockError := ks.Unlock(account, passphrase); unlockError != nil {
		return nil, nil, errors.Wrap(ErrNoUnlockBadPassphrase, unlockError.Error())
	}
	return ks, &account, nil
}
