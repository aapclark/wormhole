package querystaking

import (
	"context"
	"time"

	"github.com/certusone/wormhole/node/pkg/query/queryratelimit"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// CreateStakingPolicyProvider creates a PolicyProvider configured for staking-based rate limits
func CreateStakingPolicyProvider(ethClient *ethclient.Client, logger *zap.Logger, parentContext context.Context, factoryAddress common.Address, ipfsGateway string) (*queryratelimit.PolicyProvider, error) {
	// Create IPFS client
	// Timeout and cache size use hardcoded defaults (30s timeout is generous for both local and public gateways)
	ipfsClient := NewIPFSClient(ipfsGateway, 30*time.Second, logger)

	stakingClient := NewStakingClient(ethClient, logger, factoryAddress, ipfsClient)

	// Create the fetcher function that queries staking contracts
	// TODO: This currently only supports self-staking (signerAddr == stakerAddr).
	// To support delegated signers, the PolicyProvider interface and query request format
	// must be updated to pass both staker and signer addresses.
	fetcher := func(ctx context.Context, signerAddr common.Address) (*queryratelimit.Policy, error) {
		// For now, assume self-staking: the signer is also the staker
		return stakingClient.FetchStakingPolicy(ctx, signerAddr, signerAddr)
	}

	// Create PolicyProvider with staking fetcher
	return queryratelimit.NewPolicyProvider(
		queryratelimit.WithPolicyProviderFetcher(fetcher),
		queryratelimit.WithPolicyProviderLogger(logger.With(zap.String("component", "staking-policy-provider"))),
		queryratelimit.WithPolicyProviderParentContext(parentContext),
		queryratelimit.WithPolicyProviderOptimistic(true), // Enable background cache refresh
	)
}
