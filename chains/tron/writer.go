// SPDX-License-Identifier: LGPL-3.0-only

package tron

import (
	"github.com/cryptoveteran015/chainbridge-utils/core"
	metrics "github.com/cryptoveteran015/chainbridge-utils/metrics/types"
	"github.com/cryptoveteran015/chainbridge-utils/msg"
	"github.com/cryptoveteran015/log15"
)

var _ core.Writer = &writer{}

var PassedStatus uint8 = 2
var TransferredStatus uint8 = 3
var CancelledStatus uint8 = 4

type writer struct {
	cfg            Config
	conn           *Connection
	bridgeContract string // instance of bound receiver bridgeContract
	log            log15.Logger
	stop           <-chan int
	sysErr         chan<- error // Reports fatal error to core
	metrics        *metrics.ChainMetrics
}

// // NewWriter creates and returns writer
func NewWriter(conn *Connection, cfg *Config, log log15.Logger, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics) *writer {
	return &writer{
		cfg:     *cfg,
		conn:    conn,
		log:     log,
		stop:    stop,
		sysErr:  sysErr,
		metrics: m,
	}
}

func (w *writer) start() error {
	w.log.Debug("Starting ethereum writer...")
	return nil
}
func (w *writer) setContract(bridge string) {
	w.bridgeContract = bridge
}

func (w *writer) ResolveMessage(m msg.Message) bool {

	w.log.Info("Attempting to resolve message", "type", m.Type, "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce, "rId", m.ResourceId.Hex())
	switch m.Type {
	case msg.FungibleTransfer:
		return w.createErc20Proposal(m)
	case msg.NonFungibleTransfer:
		return w.createErc721Proposal(m)
	case msg.GenericTransfer:
		return w.createGenericDepositProposal(m)
	default:
		w.log.Error("Unknown message type received", "type", m.Type)
		return false
	}
}
