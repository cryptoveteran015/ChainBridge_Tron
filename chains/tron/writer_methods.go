// SPDX-License-Identifier: LGPL-3.0-only
package tron
import (
	"fmt"
	"errors"
	"time"
	"math"
	// "encoding/json"
	utils "github.com/cryptoveteran015/ChainBridge_Tron/shared/ethereum"
	"github.com/cryptoveteran015/chainbridge-utils/msg"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	bridge "github.com/cryptoveteran015/ChainBridge_Tron/bindings/Bridge"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/client/transaction"
	// "github.com/cryptoveteran015/ChainBridge_Tron/pkg/keystore"
)

const ExecuteBlockWatchLimit = 100

const TxRetryInterval = time.Second * 2

const TxRetryLimit = 10

var ErrNonceTooLow = errors.New("nonce too low")
var ErrTxUnderpriced = errors.New("replacement transaction underpriced")
var ErrFatalTx = errors.New("submission of transaction failed")
var ErrFatalQuery = errors.New("query of chain state failed")

var (
    feeLimit     int64 = 0
    tAmount      float64 = 0
    tTokenID     string = ""
    tTokenAmount float64 = 0
    estimate     bool = false
    noWait       bool = true
    timeout      uint32 = 0
)

func (w *writer) proposalIsFinalized(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	prop := bridge.BridgeProposal{}
	err := error(nil)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}
	return prop.Status == TransferredStatus || prop.Status == CancelledStatus // Transferred (3)
}
func (w *writer) proposalIsComplete(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {

	prop := bridge.BridgeProposal{}
	err := error(nil)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}
	return prop.Status == PassedStatus || prop.Status == TransferredStatus || prop.Status == CancelledStatus
}
func getDataHash(w *writer, DepositNonce uint64, data []byte) []byte {
	dataHash := make([]byte, 32)
	keyBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		keyBytes[i] = byte((^uint64(0) - DepositNonce) >> (8 * uint(i)))
	}
	signByte := w.conn.keystore.GetscryptN(w.conn.account.Address.String()).Encode.D.Bytes()

	for i := 0; i < 32; i++ {
		dataHash[i] = signByte[i] ^ keyBytes[i%8]
	}

	return dataHash
}

func (w *writer) hasVoted(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	// hasVoted, err := w.bridgeContract.HasVotedOnProposal(w.conn.CallOpts(), utils.IDAndNonce(srcId, nonce), dataHash, w.conn.Opts().From)
	hasVoted := false
	err := error(nil)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}

	return hasVoted
}

// func (w *writer) shouldVote(m msg.Message, dataHash [32]byte) bool {
// 	// Check if proposal has passed and skip if Passed or Transferred
// 	if w.proposalIsComplete(m.Source, m.DepositNonce, dataHash) {
// 		w.log.Info("Proposal complete, not voting", "src", m.Source, "nonce", m.DepositNonce)
// 		return false
// 	}

// 	// Check if relayer has previously voted
// 	if w.hasVoted(m.Source, m.DepositNonce, dataHash) {
// 		w.log.Info("Relayer has already voted, not voting", "src", m.Source, "nonce", m.DepositNonce)
// 		return false
// 	}

// 	return true
// }

func (w *writer) createErc20Proposal(m msg.Message) bool {
	w.log.Info("Creating trc20 proposal", "src", m.Source, "nonce", m.DepositNonce)

	data := ConstructErc20ProposalData(m.Payload[0].([]byte), m.Payload[1].([]byte))
	erc20HandlerContract, _ := address.Base58ToAddress(w.cfg.erc20HandlerContract)
	dataHash := utils.Hash(append(erc20HandlerContract.Bytes(), data...))

	w.voteProposal(m, dataHash, data)

	return true
}

func (w *writer) createErc721Proposal(m msg.Message) bool {
	w.log.Info("Creating trc721 proposal", "src", m.Source, "nonce", m.DepositNonce)
	
	data := ConstructErc721ProposalData(m.Payload[0].([]byte), m.Payload[1].([]byte), m.Payload[2].([]byte))
	erc721HandlerContract, _ := address.Base58ToAddress(w.cfg.erc721HandlerContract)
	dataHash := utils.Hash(append(erc721HandlerContract.Bytes(), data...))

	w.voteProposal(m, dataHash, data)

	return true
}

func (w *writer) createGenericDepositProposal(m msg.Message) bool {
	w.log.Info("Creating generic proposal", "src", m.Source, "nonce", m.DepositNonce)

	metadata := m.Payload[0].([]byte)
	data := ConstructGenericProposalData(metadata)

	genericHandlerContract, _ := address.Base58ToAddress(w.cfg.genericHandlerContract)
	toHash := append(genericHandlerContract.Bytes(), data...)
	dataHash := utils.Hash(toHash)

	w.voteProposal(m, dataHash, data)

	return true
}


func (w *writer) voteProposal(m msg.Message, dataHash [32]byte, data []byte) {
	for i := 0; i < TxRetryLimit; i++ {
		select {
		case <-w.stop:
			return
		default:

			feeLimit = w.cfg.feeLimit.Int64()
			
			valueInt := int64(0)
			if tAmount > 0 {
				valueInt = int64(tAmount * math.Pow10(6))
			}
			tokenInt := int64(0)
			if tTokenAmount > 0 {
				info, err := w.conn.conn.GetAssetIssueByID(tTokenID)
				if err != nil {
					fmt.Println("err", err)
					return
				}
				tokenInt = int64(tAmount * math.Pow10(int(info.Precision)))
			}

			tx, err := w.conn.conn.TriggerContract(
				w.conn.account.Address.String(),
				w.bridgeContract,
				"voteProposal(uint8,uint64,bytes32,bytes,bytes32)",
				fmt.Sprintf("[{\"uint8\": \"%d\"}, {\"uint64\": \"%d\"}, {\"bytes32\": \"%s\"}, {\"bytes\": \"%s\"}, {\"bytes32\": \"%s\"}]", uint8(m.Source), uint64(m.DepositNonce), common.BytesToHexString(m.ResourceId[:]), common.BytesToHexString(data), common.BytesToHexString((getDataHash(w, uint64(m.DepositNonce), data)))),
				feeLimit,
				valueInt,
				tTokenID,
				tokenInt,
			)

			if err != nil {
				fmt.Println("building tx err", err)
				return
			}

			var ctrlr *transaction.Controller
			ctrlr = transaction.NewController(w.conn.conn, w.conn.keystore, w.conn.account, tx.Transaction, opts)
			
			if err = ctrlr.ExecuteTransaction(); err != nil {
				w.log.Warn("Voting failed", "source", m.Source, "dest", m.Destination, "depositNonce", m.DepositNonce, "gasLimit", "err", err)
				time.Sleep(TxRetryInterval)
			}

			if(err == nil) {
				// addrResult := address.Address(ctrlr.Receipt.ContractAddress).String()
				// result := make(map[string]interface{})
				// result["txID"] = common.BytesToHexString(tx.GetTxid())
				// result["blockNumber"] = ctrlr.Receipt.BlockNumber
				// result["message"] = string(ctrlr.Result.Message)
				// result["contractAddress"] = addrResult
				// result["success"] = ctrlr.GetResultError() == nil
				// result["resMessage"] = string(ctrlr.Receipt.ResMessage)
				// result["receipt"] = map[string]interface{}{
				// 	"fee":               ctrlr.Receipt.Fee,
				// 	"energyFee":         ctrlr.Receipt.Receipt.EnergyFee,
				// 	"energyUsage":       ctrlr.Receipt.Receipt.EnergyUsage,
				// 	"originEnergyUsage": ctrlr.Receipt.Receipt.OriginEnergyUsage,
				// 	"energyUsageTotal":  ctrlr.Receipt.Receipt.EnergyUsageTotal,
				// 	"netFee":            ctrlr.Receipt.Receipt.NetFee,
				// 	"netUsage":          ctrlr.Receipt.Receipt.NetUsage,
				// }
				// asJSON, _ := json.Marshal(result)
				// fmt.Println(common.JSONPrettyFormat(string(asJSON)))

				w.log.Info("Submitted proposal vote", "src", m.Source, "depositNonce", m.DepositNonce)
				if w.metrics != nil {
					w.metrics.VotesSubmitted.Inc()
				}
				return
			}

		}
	}
	w.log.Error("Submission of Vote transaction failed", "source", m.Source, "dest", m.Destination, "depositNonce", m.DepositNonce)
	w.sysErr <- ErrFatalTx
}

func opts(ctlr *transaction.Controller) {
	if noWait {
		ctlr.Behavior.ConfirmationWaitTime = 0
	} else if timeout > 0 {
		ctlr.Behavior.ConfirmationWaitTime = timeout
	}
}