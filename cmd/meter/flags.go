// Copyright (c) 2018 The VeChainThor developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"github.com/inconshreveable/log15"
	cli "gopkg.in/urfave/cli.v1"
)

var (
	networkFlag = cli.StringFlag{
		Name:  "network",
		Usage: "the network to join (main|test)",
	}
	configDirFlag = cli.StringFlag{
		Name:   "config-dir",
		Value:  defaultConfigDir(),
		Hidden: true,
		Usage:  "directory for user global configurations",
	}
	dataDirFlag = cli.StringFlag{
		Name:  "data-dir",
		Value: defaultDataDir(),
		Usage: "directory for block-chain databases",
	}
	beneficiaryFlag = cli.StringFlag{
		Name:  "beneficiary",
		Usage: "address for block rewards",
	}
	apiAddrFlag = cli.StringFlag{
		Name:  "api-addr",
		Value: "localhost:8669",
		Usage: "API service listening address",
	}
	apiCorsFlag = cli.StringFlag{
		Name:  "api-cors",
		Value: "",
		Usage: "comma separated list of domains from which to accept cross origin requests to API",
	}
	apiTimeoutFlag = cli.IntFlag{
		Name:  "api-timeout",
		Value: 10000,
		Usage: "API request timeout value in milliseconds",
	}
	apiCallGasLimitFlag = cli.IntFlag{
		Name:  "api-call-gas-limit",
		Value: 50000000,
		Usage: "limit contract call gas",
	}
	apiBacktraceLimitFlag = cli.IntFlag{
		Name:  "api-backtrace-limit",
		Value: 1000,
		Usage: "limit the distance between 'position' and best block for subscriptions APIs",
	}
	verbosityFlag = cli.IntFlag{
		Name:  "verbosity",
		Value: int(log15.LvlInfo),
		Usage: "log verbosity (0-9)",
	}
	peersFlag = cli.StringSliceFlag{
		Name:  "peers, P",
		Usage: "P2P peers in enode format",
	}
	maxPeersFlag = cli.IntFlag{
		Name:  "max-peers",
		Usage: "maximum number of P2P network peers (P2P network disabled if set to 0)",
		Value: 25,
	}
	p2pPortFlag = cli.IntFlag{
		Name:  "p2p-port",
		Value: 11235,
		Usage: "P2P network listening port",
	}
	natFlag = cli.StringFlag{
		Name:  "nat",
		Value: "any",
		Usage: "port mapping mechanism (any|none|upnp|pmp|extip:<IP>)",
	}
	onDemandFlag = cli.BoolFlag{
		Name:  "on-demand",
		Usage: "create new block when there is pending transaction",
	}
	persistFlag = cli.BoolFlag{
		Name:  "persist",
		Usage: "blockchain data storage option, if setted data will be saved to disk",
	}
	gasLimitFlag = cli.IntFlag{
		Name:  "gas-limit",
		Value: 200000000,
		Usage: "block gas limit",
	}
	importMasterKeyFlag = cli.BoolFlag{
		Name:  "import",
		Usage: "import master key from keystore",
	}
	exportMasterKeyFlag = cli.BoolFlag{
		Name:  "export",
		Usage: "export master key to keystore",
	}
	generateKFrameFlag = cli.BoolFlag{
		Name:  "gen-kframe",
		Usage: "start a coroutine for kframe generation (FOR TEST ONLY)",
	}
	forceLastKFrameFlag = cli.BoolFlag{
		Name:  "force-last-kframe",
		Usage: "force to use last K frame",
	}
	skipSignatureCheckFlag = cli.BoolFlag{
		Name:  "skip-signature-check",
		Usage: "skip signature check",
	}
	powNodeFlag = cli.StringFlag{
		Name:  "pow-node",
		Usage: "address of pow node",
		Value: "localhost",
	}
	powPortFlag = cli.IntFlag{
		Name:  "pow-port",
		Usage: "port of pow node",
		Value: 8332,
	}
	powUserFlag = cli.StringFlag{
		Name:  "pow-user",
		Usage: "user of pow node",
		Value: "testuser",
	}
	powPassFlag = cli.StringFlag{
		Name:  "pow-pass",
		Usage: "password of pow node",
		Value: "testpass",
	}
	noDiscoverFlag = cli.BoolFlag{
		Name:  "no-discover",
		Usage: "disable auto discovery mode",
	}
	committeeSizeFlag = cli.IntFlag{
		Name:  "committee-size",
		Usage: "committee size",
		Value: 2,
	}
	delegateSizeFlag = cli.IntFlag{
		Name:  "delegate-size",
		Usage: "delegate size",
		Value: 2,
	}
	discoServerFlag = cli.StringSliceFlag{
		Name:  "disco-server",
		Usage: "override the default discover servers setting",
	}
)