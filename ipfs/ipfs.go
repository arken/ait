package ipfs

import (
	"context"
	"fmt"
	"github.com/arkenproject/ait/utils"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	aitConf "github.com/arkenproject/ait/config"

	config "github.com/ipfs/go-ipfs-config"
	libp2p "github.com/ipfs/go-ipfs/core/node/libp2p"
	migrate "github.com/ipfs/go-ipfs/repo/fsrepo/migrations"
	icore "github.com/ipfs/interface-go-ipfs-core"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/coreapi"
	"github.com/ipfs/go-ipfs/peering"
	"github.com/ipfs/go-ipfs/plugin/loader" // This package is needed so that all the preloaded plugins are loaded automatically
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	"github.com/libp2p/go-libp2p-core/peer"
)

var (
	ipfs   icore.CoreAPI
	node   *core.IpfsNode
	ctx    context.Context
	cancel context.CancelFunc
	ps     *peering.PeeringService
	// AtRiskThreshhold is the number of peers for a piece
	// of data to be backed up on to be considered safe.
	AtRiskThreshhold int
)

func init() {
	var err error
	ctx, cancel = context.WithCancel(context.Background())

	ipfs, err = spawnNode(ctx, aitConf.Global.IPFS.Path)
	utils.CheckError(err)
	ps = peering.NewPeeringService(node.PeerHost)

	bootstrapNodes := []string{
		// IPFS Bootstrapper nodes.
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
		"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",

		// IPFS Cluster Pinning nodes
		"/ip4/138.201.67.219/tcp/4001/p2p/QmUd6zHcbkbcs7SMxwLs48qZVX3vpcM8errYS7xEczwRMA",
		"/ip4/138.201.67.219/udp/4001/quic/p2p/QmUd6zHcbkbcs7SMxwLs48qZVX3vpcM8errYS7xEczwRMA",
		"/ip4/138.201.68.74/tcp/4001/p2p/QmdnXwLrC8p1ueiq2Qya8joNvk3TVVDAut7PrikmZwubtR",
		"/ip4/138.201.68.74/udp/4001/quic/p2p/QmdnXwLrC8p1ueiq2Qya8joNvk3TVVDAut7PrikmZwubtR",
		"/ip4/94.130.135.167/tcp/4001/p2p/QmUEMvxS2e7iDrereVYc5SWPauXPyNwxcy9BXZrC1QTcHE",
		"/ip4/94.130.135.167/udp/4001/quic/p2p/QmUEMvxS2e7iDrereVYc5SWPauXPyNwxcy9BXZrC1QTcHE",

		// Arken Bootstrapper node.
		"/dns4/link.arken.io/tcp/4001/ipfs/QmP8krSfWWHLNL2eah6E1hr6TzoaGMEVRw2Fooy5og1Wpj",
	}

	go connectToPeers(ctx, ipfs, bootstrapNodes)
	ps.Start()
}

// GetID returns the identifier of the node.
func GetID() (result string) {
	return node.Identity.Pretty()
}

// GetRepoSize returns the size of the repo in bytes.
func GetRepoSize() (result uint64, err error) {
	out, err := node.Repo.GetStorageUsage()
	if err != nil {
		return result, err
	}
	return out, nil
}

func setupPlugins(externalPluginsPath string) error {
	// Load any external plugins if available on externalPluginsPath
	plugins, err := loader.NewPluginLoader(filepath.Join(externalPluginsPath, "plugins"))
	if err != nil {
		return fmt.Errorf("error loading plugins: %s", err)
	}

	// Load preloaded and external plugins
	if err := plugins.Initialize(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	if err := plugins.Inject(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	return nil
}

// Spawns an IPFS node creating the config/storage repository if it doesn't already exist.
func spawnNode(ctx context.Context, path string) (icore.CoreAPI, error) {

	if err := setupPlugins(path); err != nil {
		return nil, err
	}

	ipfs, err := createNode(ctx, path)
	if err != nil {
		path, err = createRepo(ctx, path)
		if err != nil {
			return nil, err
		}
		return createNode(ctx, path)
	}
	return ipfs, err
}

// Creates an IPFS node and returns its coreAPI
func createNode(ctx context.Context, repoPath string) (icore.CoreAPI, error) {
	// Open the repo
	repo, err := fsrepo.Open(repoPath)
	if err != nil {
		if err == fsrepo.ErrNeedMigration {
			migrate.DistPath = repoPath
			err = migrate.RunMigration(fsrepo.RepoVersion)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Construct the node

	nodeOptions := &core.BuildCfg{
		Permanent: true,
		Online:    true,
		Routing:   libp2p.DHTOption, // This option sets the node to be a full DHT node (both fetching and storing DHT Records)
		// Routing: libp2p.DHTClientOption, // This option sets the node to be a client DHT node (only fetching records)
		Repo: repo,
	}

	node, err = core.NewNode(ctx, nodeOptions)
	if err != nil {
		return nil, err
	}

	node.IsDaemon = true

	// Attach the Core API to the constructed node
	return coreapi.NewCoreAPI(node)
}

// Bootstraps the initial system by connecting the node to known IPFS peers.
func connectToPeers(ctx context.Context, ipfs icore.CoreAPI, peers []string) error {
	var wg sync.WaitGroup
	peerInfos := make(map[peer.ID]*peerstore.PeerInfo, len(peers))
	for _, addrStr := range peers {
		addr, err := ma.NewMultiaddr(addrStr)
		if err != nil {
			return err
		}
		pii, err := peerstore.InfoFromP2pAddr(addr)
		if err != nil {
			return err
		}
		pi, ok := peerInfos[pii.ID]
		if !ok {
			pi = &peerstore.PeerInfo{ID: pii.ID}
			peerInfos[pi.ID] = pi
		}
		pi.Addrs = append(pi.Addrs, pii.Addrs...)
	}

	wg.Add(len(peerInfos))
	for _, peerInfo := range peerInfos {
		go func(peerInfo *peerstore.PeerInfo) {
			defer wg.Done()
			err := ipfs.Swarm().Connect(ctx, *peerInfo)
			if err != nil {
				fmt.Printf("failed to connect to %s: %s\n", peerInfo.ID, err)
			}
		}(peerInfo)
	}
	wg.Wait()
	return nil
}

// creates the IPFS configuration repository
func createRepo(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.Mkdir(path, os.ModePerm)
	}

	// Create a config with default options and a 2048 bit key
	cfg, err := config.Init(ioutil.Discard, 2048)
	if err != nil {
		return "", err
	}

	cfg.Reprovider.Strategy = "all"
	cfg.Reprovider.Interval = "1h"
	cfg.Routing.Type = "dhtserver"
	cfg.Swarm.EnableAutoRelay = true

	// Create the repo with the config
	err = fsrepo.Init(path, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to init node: %s\n", err)
	}

	return path, nil
}
