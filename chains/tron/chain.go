// SPDX-License-Identifier: LGPL-3.0-only
package tron

import (
	"fmt"
	"strings"
	"math/big"
	"strconv"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/keystore"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/store"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/address"
	// erc20Handler "github.com/cryptoveteran015/ChainBridge_Tron/bindings/ERC20Handler"
	// erc721Handler "github.com/cryptoveteran015/ChainBridge_Tron/bindings/ERC721Handler"
	// "github.com/cryptoveteran015/ChainBridge_Tron/bindings/GenericHandler"
	// utils "github.com/cryptoveteran015/ChainBridge_Tron/shared/ethereum"
	"github.com/cryptoveteran015/chainbridge-utils/blockstore"
	"github.com/cryptoveteran015/chainbridge-utils/core"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/common"
	// "github.com/cryptoveteran015/chainbridge-utils/crypto/secp256k1"
	utils_keystore "github.com/cryptoveteran015/chainbridge-utils/keystore"
	metrics "github.com/cryptoveteran015/chainbridge-utils/metrics/types"
	"github.com/cryptoveteran015/chainbridge-utils/msg"
	"github.com/cryptoveteran015/log15"
	"github.com/cryptoveteran015/ChainBridge_Tron/pkg/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var _ core.Chain = &Chain{}

type Connection struct {
	conn                   *client.GrpcClient
	keystore               *keystore.KeyStore 
	account                *keystore.Account
	stop     			   chan int // All routines should exit when this channel is closed
	log                    log15.Logger
}

type Chain struct {
	cfg      *core.ChainConfig // The config of the chain
	conn     *Connection        // THe chains connection
	listener *listener         // The listener of this chain
	writer   *writer           // The writer of the chain
	stop     chan<- int
}
func setupBlockstore(cfg *Config, addr string) (*blockstore.Blockstore, error) {
	bs, err := blockstore.NewBlockstore(cfg.blockstorePath, cfg.id, addr)
	if err != nil {
		return nil, err
	}

	if !cfg.freshStart {
		latestBlock, err := bs.TryLoadLatestBlock()
		if err != nil {
			return nil, err
		}

		if latestBlock.Cmp(cfg.startBlock) == 1 {
			cfg.startBlock = latestBlock
		}
	}

	return bs, nil
}
func InitializeChain(chainCfg *core.ChainConfig, logger log15.Logger, sysErr chan<- error, m *metrics.ChainMetrics) (*Chain, error) {
	cfg, err := parseChainConfig(chainCfg)
	if err != nil {
		return nil, err
	}
	password := utils_keystore.GetPassword(fmt.Sprintf("Enter password for key: %s", cfg.from))
	passphrase := string(password)

	ks, acct, err := store.UnlockedKeystore(cfg.from, passphrase, cfg.keystorePath)
	
	if err != nil {
		return nil, err
	}

	addr := acct.Address.String()

	bs, err := setupBlockstore(cfg, addr)
	if err != nil {
		return nil, err
	}

	stop := make(chan int)
	conn := &Connection{
		keystore: 			ks,
		account:		 	acct,
		stop:               make(chan int),
		log:                logger,
	}

	err = conn.Connect(cfg.endpoint, cfg.trongridKey)
	if err != nil {
		return nil, err
	}
	err = conn.EnsureHasBytecode(cfg.bridgeContract)
	if err != nil {
		return nil, err
	}

	if cfg.erc20HandlerContract != "" {
		err := conn.EnsureHasBytecode(cfg.erc20HandlerContract)
		if err != nil {
			return nil, err
		}
	}

	if cfg.erc721HandlerContract != "" {
		err := conn.EnsureHasBytecode(cfg.erc721HandlerContract)
		if err != nil {
			return nil, err
		}
	}

	if cfg.genericHandlerContract != "" {
		err := conn.EnsureHasBytecode(cfg.genericHandlerContract)
		if err != nil {
			return nil, err
		}
	}

	chainId, err := conn.ChainID(cfg.bridgeContract)

	if err != nil {
		return nil, err
	}

	if chainId != uint8(chainCfg.Id) {
		return nil, fmt.Errorf("chainId (%d) and configuration chainId (%d) do not match", chainId, chainCfg.Id)
	}

	if chainCfg.LatestBlock {
		curr, err := conn.LatestBlock()
		if err != nil {
			return nil, err
		}
		cfg.startBlock = curr
	}
	listener := NewListener(conn, cfg, logger, bs, stop, sysErr, m)
	listener.setContracts(cfg.bridgeContract, cfg.erc20HandlerContract, cfg.erc721HandlerContract, cfg.genericHandlerContract)

	writer := NewWriter(conn, cfg, logger, stop, sysErr, m)
	writer.setContract(cfg.bridgeContract)

	return &Chain{
		cfg:      chainCfg,
		conn:     conn,
		writer:   writer,
		listener: listener,
		stop:     stop,
	}, nil
}

func (c *Chain) SetRouter(r *core.Router) {
	r.Listen(c.cfg.Id, c.writer)
	c.listener.setRouter(r)
}

func (c *Chain) Start() error {
	err := c.listener.start()
	if err != nil {
		return err
	}

	err = c.writer.start()
	if err != nil {
		return err
	}

	c.writer.log.Debug("Successfully started chain")
	return nil
}

func (c *Chain) Id() msg.ChainId {
	return c.cfg.Id
}

func (c *Chain) Name() string {
	return c.cfg.Name
}

func (c *Chain) LatestBlock() metrics.LatestBlock {
	return c.listener.latestBlock
}

func (c *Chain) Stop() {
	close(c.stop)
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *Connection) Connect(node string, trongridKey string) error {

	c.log.Info("Connecting to tron chain...", "url", node)
	
	switch URLcomponents := strings.Split(node, ":"); len(URLcomponents) {
	case 1:
		node = node + ":50051"
	}
	c.conn = client.NewGrpcClient(node)

	opts := make([]grpc.DialOption, 0)
	withTLS := false
	if withTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	c.conn.SetAPIKey(trongridKey)

	if err := c.conn.Start(opts...); err != nil {
		return err
	}
	return nil
}

func (c *Connection) LatestBlock() (*big.Int, error) {

	curBlock, err := c.conn.GetNowBlock()
	
	if err != nil {
		return nil, err
	}

	curBlockNum := curBlock.GetBlockHeader().GetRawData().GetNumber()
	curBlockBig := big.NewInt(curBlockNum)
	
	return curBlockBig, nil
}

func (c *Connection) EnsureHasBytecode(addr string) error {
	_, err := address.Base58ToAddress(addr)
	
	if err != nil {
		return err
	}

	return nil
}

func (c *Connection) Close() {
	if c.conn != nil {
		c.conn.Stop()
	}
	close(c.stop)
}

func (c *Connection) ChainID(bridgeContract string) (uint8, error) {
	tx, err := c.conn.TriggerConstantContract(
		c.account.Address.String(),
		bridgeContract,
		"_chainID()",
		"[]",
	)
	if err != nil {
		return uint8(0), err
	}

	cResult := tx.GetConstantResult()
	hexStr := common.ToHexWithout0x(cResult[0])


	uintVal, err := strconv.ParseUint(hexStr, 16, 8)

	if err != nil {
		return uint8(0), err 
	}

	uint8Val := uint8(uintVal)

	return uint8Val, nil
}