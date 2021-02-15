package ipfs

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	aitConf "github.com/arken/ait/config"

	config "github.com/ipfs/go-ipfs-config"
	serialize "github.com/ipfs/go-ipfs-config/serialize"
	libp2p "github.com/ipfs/go-ipfs/core/node/libp2p"
	"github.com/ipfs/go-ipfs/peering"
	migrate "github.com/ipfs/go-ipfs/repo/fsrepo/migrations"
	icore "github.com/ipfs/interface-go-ipfs-core"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/coreapi"
	"github.com/ipfs/go-ipfs/plugin/loader" // This package is needed so that all the preloaded plugins are loaded automatically
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	"github.com/libp2p/go-libp2p-core/peer"
)

var (
	// AtRiskThreshhold is the number of peers for a piece
	// of data to be backed up on to be considered safe.
	AtRiskThreshhold int
	ps               *peering.PeeringService
	ipfs             icore.CoreAPI
	node             *core.IpfsNode
	ctx              context.Context
	cancel           context.CancelFunc
)

// Init starts the IPFS subsystem.
func Init(online bool) {
	var err error
	ctx, cancel = context.WithCancel(context.Background())

	ctx, ipfs, err = spawnNode(aitConf.Global.IPFS.Path, online)
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := node.Repo.Config()
	if err != nil {
		log.Fatal(err)
	}
	cfg.Experimental.FilestoreEnabled = true
	peers := []string{
		// Arken Bootstrapper node.
		"/dns4/link.arken.io/tcp/4001/ipfs/12D3KooWSmosHZtDBbepxWwVgo8HyXSgNCUgs2GGD2qnQPbA3KhD",
		"/dns4/relay.arken.io/tcp/4001/ipfs/12D3KooWL7hvR7nfQxAWMowgoWXWQwKEkQA8QPZrhKjateRTgcDm",
	}
	go connectToPeers(ctx, ipfs, peers)

}

// spawnNode creates and tests and IPFS node for public reachability.
func spawnNode(path string, online bool) (ctx context.Context, api icore.CoreAPI, err error) {
	// Create IPFS node
	ctx, cancel = context.WithCancel(context.Background())

	err = setRelay(false, path)
	if err != nil && err.Error() != "ipfs not initialized, please run 'ipfs init'" {
		return ctx, api, err
	}
	api, err = setupNode(ctx, path)
	if err != nil {
		return ctx, api, err
	}

	if online {
		// Wait 30s before testing reachability
		fmt.Printf("[Checking Node Reachability on Arken Network]\n")
		time.Sleep(30 * time.Second)
		public, err := checkReachability(api)
		if err != nil {
			return ctx, api, err
		}
		// If the node isn't publicly reachable switch to relay system.
		if !public {
			cancel()
			fmt.Printf("[Node unable to be reached by network.]\n")
			fmt.Printf("[Recreating using Circuit Relay System.]\n")

			setRelay(true, path)

			// Wait for port to free
			time.Sleep(30 * time.Second)

			// Recreate IPFS Node
			ctx, cancel = context.WithCancel(context.Background())
			api, err = createNode(ctx, path)
			if err != nil {
				return ctx, api, err
			}
			fmt.Printf("[Node Re-Created Sucessfully]\n")

			ps = peering.NewPeeringService(node.PeerHost)
			addr, err := ma.NewMultiaddr("/dns4/relay.arken.io/tcp/4001/ipfs/12D3KooWL7hvR7nfQxAWMowgoWXWQwKEkQA8QPZrhKjateRTgcDm")
			if err != nil {
				log.Fatal(err)
			}
			ps.AddPeer(peer.AddrInfo{ID: "12D3KooWL7hvR7nfQxAWMowgoWXWQwKEkQA8QPZrhKjateRTgcDm", Addrs: []ma.Multiaddr{addr}})
			ps.Start()

		} else {
			fmt.Printf("[Arken Node is Publicly Reachable with NAT]\n")
		}
	}
	return ctx, api, nil
}

// setRelay
func setRelay(relay bool, path string) (err error) {
	cfg, err := fsrepo.ConfigAt(path)
	if err != nil {
		return err
	}
	if relay {
		cfg.Addresses.Announce = []string{
			"/dns4/relay.arken.io/tcp/4001/p2p/12D3KooWL7hvR7nfQxAWMowgoWXWQwKEkQA8QPZrhKjateRTgcDm/p2p-circuit/p2p/" + cfg.Identity.PeerID,
		}
	} else {
		cfg.Addresses.Announce = []string{}
	}
	cfg.Routing.Type = "dhtserver"

	configFilename, err := config.Filename(path)
	if err != nil {
		return err
	}
	if err := serialize.WriteConfigFile(configFilename, cfg); err != nil {
		return err
	}
	return nil
}

// checkReachability tests if the IPFS node is reachable by the network
// and opts to use a relay if it is not.
func checkReachability(api icore.CoreAPI) (public bool, err error) {
	ips := []string{"/ip4/10.", "/ip4/192.", "/ip4/127.", "/ip4/172.", "/ip6/"}

	multi, err := api.Swarm().LocalAddrs(ctx)
	if err != nil {
		return false, err
	}
	for addrNum := range multi {
		addr := multi[addrNum].String()
		private := false

		for ipNum := range ips {
			if strings.HasPrefix(addr, ips[ipNum]) {
				private = true
			}
		}
		if !private {
			// Public Address Found. Return that node is reachable.
			return true, nil
		}
	}
	// No public IPs were found. Return that node is NOT reachable.
	return false, nil
}

// GetID returns the identifier of the node.
func GetID() (result string) {
	return node.Identity.Pretty()
}

// setupPlugins loads an initializes any external plugins
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

// setupNode spawns an IPFS node creating the config/storage repository if it doesn't already exist.
func setupNode(ctx context.Context, path string) (icore.CoreAPI, error) {

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

// createNode creates an IPFS node and returns its coreAPI
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
		Repo:      repo,
	}

	node, err = core.NewNode(ctx, nodeOptions)
	if err != nil {
		return nil, err
	}

	node.IsDaemon = true

	// Attach the Core API to the constructed node
	return coreapi.NewCoreAPI(node)
}

// connectToPeers bootstraps the initial system by connecting the node to known IPFS peers.
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
				log.Printf("failed to connect to %s: %s", peerInfo.ID, err)
			}
		}(peerInfo)
	}
	wg.Wait()
	return nil
}

// createRepo creates the IPFS configuration repository
func createRepo(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.Mkdir(path, os.ModePerm)
	}

	// Create a config with default options and a 2048 bit key
	cfg, err := config.Init(ioutil.Discard, 2048)
	if err != nil {
		return "", err
	}

	cfg.Datastore.Spec = badgerSpec()
	cfg.Datastore.StorageMax = "100TB"
	cfg.Reprovider.Strategy = "roots"
	cfg.Reprovider.Interval = "1h"
	cfg.Routing.Type = "dhtserver"
	cfg.Experimental.FilestoreEnabled = true
	bootstrapNodes := []string{
		// Arken Bootstrapper node.
		"/dns4/link.arken.io/tcp/4001/ipfs/12D3KooWSmosHZtDBbepxWwVgo8HyXSgNCUgs2GGD2qnQPbA3KhD",
	}
	cfg.Bootstrap = bootstrapNodes

	// Create the repo with the config
	err = fsrepo.Init(path, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to init node: %s", err)
	}

	return path, nil
}

func badgerSpec() map[string]interface{} {
	return map[string]interface{}{
		"type":   "measure",
		"prefix": "badger.datastore",
		"child": map[string]interface{}{
			"type":       "badgerds",
			"path":       "badgerds",
			"syncWrites": false,
			"truncate":   true,
		},
	}
}
