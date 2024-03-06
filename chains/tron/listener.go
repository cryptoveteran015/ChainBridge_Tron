// SPDX-License-Identifier: LGPL-3.0-only

package tron

import (
	"errors"
	"fmt"
	"math/big"
	"time"
	"strconv"
	"bytes"
	"github.com/cryptoveteran015/ChainBridge_Tron/chains"
	"github.com/cryptoveteran015/chainbridge-utils/blockstore"
	metrics "github.com/cryptoveteran015/chainbridge-utils/metrics/types"
	"github.com/cryptoveteran015/log15"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	"github.com/cryptoveteran015/chainbridge-utils/msg"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var BlockRetryInterval = time.Second * 5
var BlockRetryLimit = 5
var ErrFatalPolling = errors.New("listener block polling failed")

type EventSig string

func (es EventSig) GetTopic() ethcommon.Hash {
	return crypto.Keccak256Hash([]byte(es))
}

const (
	DepositEvent       EventSig = "Deposit(uint8,bytes32,uint64)"
	ProposalEvent EventSig = "ProposalEvent(uint8,uint64,uint8,bytes32,bytes32)"
	ProposalVoteEvent  EventSig = "ProposalVote(uint8,uint64,uint8,bytes32)"
)

type listener struct {
	cfg                    Config
	conn                   *Connection
	router                 chains.Router
	bridgeContract         string
	erc20HandlerContract   string
	erc721HandlerContract  string
	genericHandlerContract string
	log                    log15.Logger
	blockstore             blockstore.Blockstorer
	stop                   <-chan int
	sysErr                 chan<- error // Reports fatal error to core
	latestBlock            metrics.LatestBlock
	metrics                *metrics.ChainMetrics
	blockConfirmations     *big.Int
}

// NewListener creates and returns a listener
func NewListener(conn *Connection, cfg *Config, log log15.Logger, bs blockstore.Blockstorer, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics) *listener {
	return &listener{
		cfg:                *cfg,
		conn:               conn,
		log:                log,
		blockstore:         bs,
		stop:               stop,
		sysErr:             sysErr,
		latestBlock:        metrics.LatestBlock{LastUpdated: time.Now()},
		metrics:            m,
		blockConfirmations: cfg.blockConfirmations,
	}
}

// setContracts sets the listener with the appropriate contracts
func (l *listener) setContracts(bridge string, erc20Handler string, erc721Handler string, genericHandler string) {
	l.bridgeContract = bridge
	l.erc20HandlerContract = erc20Handler
	l.erc721HandlerContract = erc721Handler
	l.genericHandlerContract = genericHandler
}

// sets the router
func (l *listener) setRouter(r chains.Router) {
	l.router = r
}

// start registers all subscriptions provided by the config
func (l *listener) start() error {
	l.log.Debug("Starting listener...")

	go func() {
		err := l.pollBlocks()
		if err != nil {
			l.log.Error("Polling blocks failed", "err", err)
		}
	}()

	return nil
}

func (l *listener) pollBlocks() error {
	var currentBlock = l.cfg.startBlock
	l.log.Info("Polling Blocks...", "block", currentBlock)

	var retry = BlockRetryLimit
	for {
		select {
		case <-l.stop:
			return errors.New("polling terminated")
		default:
			if retry == 0 {
				l.log.Error("Polling failed, retries exceeded")
				l.sysErr <- ErrFatalPolling
				return nil
			}

			latestBlock, err := l.conn.LatestBlock()
			if err != nil {
				l.log.Error("Unable to get latest block", "block", currentBlock, "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			if l.metrics != nil {
				l.metrics.LatestKnownBlock.Set(float64(latestBlock.Int64()))
			}

			if big.NewInt(0).Sub(latestBlock, currentBlock).Cmp(l.blockConfirmations) == -1 {
				l.log.Debug("Block not ready, will retry", "target", currentBlock, "latest", latestBlock)
				time.Sleep(BlockRetryInterval)
				continue
			}

			err = l.getDepositEventsForBlock(currentBlock)
			if err != nil {
				l.log.Error("Failed to get events for block", "block", currentBlock, "err", err)
				retry--
				continue
			}

			err = l.blockstore.StoreBlock(currentBlock)
			if err != nil {
				l.log.Error("Failed to write latest block to blockstore", "block", currentBlock, "err", err)
			}

			if l.metrics != nil {
				l.metrics.BlocksProcessed.Inc()
				l.metrics.LatestProcessedBlock.Set(float64(latestBlock.Int64()))
			}

			l.latestBlock.Height = big.NewInt(0).Set(latestBlock)
			l.latestBlock.LastUpdated = time.Now()

			// Goto next block and reset retry counter
			currentBlock.Add(currentBlock, big.NewInt(1))
			retry = BlockRetryLimit
		}
	}
}

func (l *listener) getDepositEventsForBlock(latestBlock *big.Int) error {
	l.log.Debug("Querying block for deposit events", "block", latestBlock)
	
	txInfoList, _ := l.conn.conn.GetBlockInfoByNum(latestBlock.Int64())

	for _, txInfo := range txInfoList.GetTransactionInfo() {
		for _, log := range txInfo.GetLog() {

			var m msg.Message

			bridgeContractAddress, _ := address.Base58ToAddress(l.bridgeContract)
			
			if bridgeContractAddress.HexInETH() != common.BytesToHexString(log.GetAddress()) {
				continue
			}	
			
			topics := log.GetTopics()
			topicByte32 := DepositEvent.GetTopic()

			if !bytes.Equal(topics[0], topicByte32[:]) {
				continue
			}

			chainIdStrHex := common.ToHexWithout0x(topics[1])
			uintVal, _:= strconv.ParseUint(chainIdStrHex, 16, 8)
			destId := msg.ChainId(uintVal)

			rId := msg.ResourceIdFromSlice(topics[2])

			nonceStrHex := common.ToHexWithout0x(topics[3])
			uintVal, _ = strconv.ParseUint(nonceStrHex, 16, 8)
			nonce := msg.Nonce(uintVal)

			addr, err := l.ResourceIDToHandlerAddress(rId.Hex())

			if err != nil {
				return fmt.Errorf("failed to get handler from resource ID %x", "rid")
			}
			erc20HandlerAddress, _ := address.Base58ToAddress(l.cfg.erc20HandlerContract)
			// erc721HandlerAddress, _ := address.Base58ToAddress(l.cfg.erc721HandlerContract)
			// genericHandlerAddress, _ := address.Base58ToAddress(l.cfg.genericHandlerContract)
			
			if common.MatchHex(addr, erc20HandlerAddress.HexInETH()) {
				m, err = l.handleErc20DepositedEvent(destId, nonce)
			// } else if common.MatchHex(addr, erc721HandlerAddress.HexInETH()) {
			// 	m, err = l.handleErc721DepositedEvent(destId, nonce)
			// } else if common.MatchHex(addr, genericHandlerAddress.HexInETH()) {
			// 	m, err = l.handleGenericDepositedEvent(destId, nonce)
			} else {
				l.log.Error("event has unrecognized handler", "handler")
				return nil
			}

			if err != nil {
				return err
			}

			err = l.router.Send(m)
			if err != nil {
				l.log.Error("subscription error: failed to route message", "err", err)
			}
		}
	}
	return nil
}
func (l *listener) ResourceIDToHandlerAddress(rId string) (string, error)  {
	tx, err := l.conn.conn.TriggerConstantContract(
		l.conn.account.Address.String(),
		l.bridgeContract,
		"_resourceIDToHandlerAddress(bytes32)",
		fmt.Sprintf("[{\"bytes32\": \"%s\"}]", rId),
	)
	if err != nil {
		return "", err
	}

	cResult := tx.GetConstantResult()
	res := common.ToHex(cResult[0])

	return res, nil
}