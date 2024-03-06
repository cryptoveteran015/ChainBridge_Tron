package account

import (
	"encoding/hex"
	"fmt"
	"io"
	// "io/ioutil"
	"os"
	"path/filepath"
	"strings"

	// "github.com/mitchellh/go-homedir"

	"github.com/btcsuite/btcd/btcec/v2"
	// mapset "github.com/deckarep/golang-set"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	"github.com/cryptoveteran015/chainbridge-utils/keystore"
	// "github.com/cryptoveteran015/ChainBridge_Tron/pkg/mnemonic"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/store"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	"github.com/cryptoveteran015/ChainBridge_Tron/config"
)

// ImportFromPrivateKey allows import of an ECDSA private key
func ImportFromPrivateKey(privateKey string, datadir string, password []byte) (string, error) {
	
	if password == nil {
		password = keystore.GetPassword("Enter password to encrypt keystore file:")
	}
	passphrase := string(password)
	
	keystorepath, err := keystoreDir(datadir)

	privateKey = strings.TrimPrefix(privateKey, "0x")
	privateKeyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return "", err
	}
	if len(privateKeyBytes) != common.Secp256k1PrivateKeyBytesLength {
		return "", common.ErrBadKeyLength
	}

	// btcec.PrivKeyFromBytes only returns a secret key and public key
	sk, _ := btcec.PrivKeyFromBytes(privateKeyBytes)
	addr := address.PubkeyToAddress(*sk.PubKey().ToECDSA())
	fmt.Println("addr.String()", addr.String())
	if store.DoesNamedAccountExist(addr.String(), keystorepath) {
		return "", fmt.Errorf("account %s already exists", addr.String())
	}
	
	ks := store.FromAccountName(addr.String(), keystorepath)
	_, err = ks.ImportECDSA(sk.ToECDSA(), passphrase)
	return addr.String(), err
}

// func generateName() string {
// 	words := strings.Split(mnemonic.Generate(), " ")
// 	existingAccounts := mapset.NewSet()
// 	for a := range store.LocalAccounts() {
// 		existingAccounts.Add(a)
// 	}
// 	foundName := false
// 	acct := ""
// 	i := 0
// 	for {
// 		if foundName {
// 			break
// 		}
// 		if i == len(words)-1 {
// 			words = strings.Split(mnemonic.Generate(), " ")
// 		}
// 		candidate := words[i]
// 		if !existingAccounts.Contains(candidate) {
// 			foundName = true
// 			acct = candidate
// 			break
// 		}
// 	}
// 	return acct
// }

func writeToFile(path string, data string) error {
	currDir, _ := os.Getwd()
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0777)
	os.Chdir(filepath.Dir(path))
	file, err := os.Create(filepath.Base(path))
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	if err != nil {
		return err
	}
	os.Chdir(currDir)
	return file.Sync()
}

// ImportKeyStore imports a keystore along with a password
// func ImportKeyStore(keyPath, name, passphrase string) (string, error) {
// 	keyPath, err := filepath.Abs(keyPath)
// 	if err != nil {
// 		return "", err
// 	}
// 	keyJSON, readError := ioutil.ReadFile(keyPath)
// 	if readError != nil {
// 		return "", readError
// 	}
// 	if name == "" {
// 		name = generateName() + "-imported"
// 		for store.DoesNamedAccountExist(name) {
// 			name = generateName() + "-imported"
// 		}
// 	} else if store.DoesNamedAccountExist(name) {
// 		return "", fmt.Errorf("account %s already exists", name)
// 	}
// 	key, err := keystore.DecryptKey(keyJSON, passphrase)
// 	if err != nil {
// 		return "", err
// 	}

// 	hasAddress := store.FromAddress(key.Address.String()) != nil
// 	if hasAddress {
// 		return "", fmt.Errorf("address %s already exists in keystore", key.Address.String())
// 	}
// 	uDir, _ := homedir.Dir()
// 	newPath := filepath.Join(uDir, common.DefaultConfigDirName, common.DefaultConfigAccountAliasesDirName, name, filepath.Base(keyPath))
// 	err = writeToFile(newPath, string(keyJSON))
// 	if err != nil {
// 		return "", err
// 	}
// 	return name, nil
// }

// keystoreDir returnns the absolute filepath of the keystore directory given a datadir
// by default, it is ./keys/
// otherwise, it is datadir/keys/
func keystoreDir(keyPath string) (keystorepath string, err error) {
	// datadir specified, return datadir/keys as absolute path
	if keyPath != "" {
		keystorepath, err = filepath.Abs(keyPath)
		if err != nil {
			return "", err
		}
	} else {
		// datadir not specified, use default
		keyPath = config.DefaultKeystorePath

		keystorepath, err = filepath.Abs(keyPath)
		if err != nil {
			return "", fmt.Errorf("could not create keystore file path: %w", err)
		}
	}

	// if datadir does not exist, create it
	if _, err = os.Stat(keyPath); os.IsNotExist(err) {
		err = os.Mkdir(keyPath, os.ModePerm)
		if err != nil {
			return "", err
		}
	}

	// if datadir/keystore does not exist, create it
	if _, err = os.Stat(keystorepath); os.IsNotExist(err) {
		err = os.Mkdir(keystorepath, os.ModePerm)
		if err != nil {
			return "", err
		}
	}

	return keystorepath, nil
}