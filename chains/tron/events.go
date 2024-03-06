// SPDX-License-Identifier: LGPL-3.0-only

package tron

import (
	"encoding/json"
	"math/big"
	"fmt"
	"strings"
	"github.com/cryptoveteran015/chainbridge-utils/msg"
	// pkgCommon "github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type ERC20HandlerDepositRecord struct {
	TokenAddress                   common.Address `json:"_tokenAddress"`
	LenDestinationRecipientAddress uint8          `json:"_lenDestinationRecipientAddress"`
	DestinationChainID             uint8          `json:"_destinationChainID"`
	ResourceID                    [32]byte      `json:"_resourceID"`
	DestinationRecipientAddress   []byte        `json:"_destinationRecipientAddress"`
	Depositer                     common.Address `json:"_depositer"`
	Amount                        *big.Int       `json:"_amount"`
}

func (l *listener) handleErc20DepositedEvent(destId msg.ChainId, nonce msg.Nonce) (msg.Message, error) {
	l.log.Info("Handling fungible deposit event", "dest", destId, "nonce", nonce)
	
	tx, err := l.conn.conn.TriggerConstantContract(
		l.conn.account.Address.String(),
		l.erc20HandlerContract,
		"getDepositRecord(uint64,uint8)",
		fmt.Sprintf("[{\"uint64\": \"%d\"}, {\"uint8\": \"%d\"}]", uint64(nonce), uint8(destId)),
	)

	
	cResult := tx.GetConstantResult()

	const abiJSON  = `[{"inputs":[{"internalType":"uint64","name":"depositNonce","type":"uint64"},{"internalType":"uint8","name":"destId","type":"uint8"}],"name":"getDepositRecord","outputs":[{"components":[{"internalType":"address","name":"_tokenAddress","type":"address"},{"internalType":"uint8","name":"_lenDestinationRecipientAddress","type":"uint8"},{"internalType":"uint8","name":"_destinationChainID","type":"uint8"},{"internalType":"bytes32","name":"_resourceID","type":"bytes32"},{"internalType":"bytes","name":"_destinationRecipientAddress","type":"bytes"},{"internalType":"address","name":"_depositer","type":"address"},{"internalType":"uint256","name":"_amount","type":"uint256"}],"internalType":"struct ERC20Handler.DepositRecord","name":"","type":"tuple"}],"stateMutability":"view","type":"function"}]`
	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		// t.Fatal(err)
		fmt.Println("err:", err)
	}

	// Unpack the returned byte array into the struct using the ABI
	out, err := parsedABI.Unpack("getDepositRecord", cResult[0])

	js, err := json.Marshal(out[0])
	if err != nil {
		fmt.Println("error:", err)
	}
	var record ERC20HandlerDepositRecord

	err = json.Unmarshal([]byte(js), &record)

	if err != nil {
		l.log.Error("Error Unpacking ERC20 Deposit Record", "err", err)
		return msg.Message{}, err
	}
	return msg.NewFungibleTransfer(
		l.cfg.id,
		destId,
		nonce,
		record.Amount,
		record.ResourceID,
		record.DestinationRecipientAddress,
	), nil
}

// func (l *listener) handleErc721DepositedEvent(destId msg.ChainId, nonce msg.Nonce) (msg.Message, error) {
// 	l.log.Info("Handling nonfungible deposit event")

// 	record, err := l.erc721HandlerContract.GetDepositRecord(&bind.CallOpts{From: l.conn.Keypair().CommonAddress()}, uint64(nonce), uint8(destId))
// 	if err != nil {
// 		l.log.Error("Error Unpacking ERC721 Deposit Record", "err", err)
// 		return msg.Message{}, err
// 	}

// 	return msg.NewNonFungibleTransfer(
// 		l.cfg.id,
// 		destId,
// 		nonce,
// 		record.ResourceID,
// 		record.TokenID,
// 		record.DestinationRecipientAddress,
// 		record.MetaData,
// 	), nil
// }

// func (l *listener) handleGenericDepositedEvent(destId msg.ChainId, nonce msg.Nonce) (msg.Message, error) {
// 	l.log.Info("Handling generic deposit event")

// 	record, err := l.genericHandlerContract.GetDepositRecord(&bind.CallOpts{From: l.conn.Keypair().CommonAddress()}, uint64(nonce), uint8(destId))
// 	if err != nil {
// 		l.log.Error("Error Unpacking Generic Deposit Record", "err", err)
// 		return msg.Message{}, nil
// 	}

// 	return msg.NewGenericTransfer(
// 		l.cfg.id,
// 		destId,
// 		nonce,
// 		record.ResourceID,
// 		record.MetaData[:],
// 	), nil
// }
