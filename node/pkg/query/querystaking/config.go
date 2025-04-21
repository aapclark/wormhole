package querystaking

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// PoolAddresses holds the configured staking pool addresses
type PoolAddresses struct {
	EVMPool        common.Address
	SolanaPool     common.Address
	FactoryAddress common.Address // Factory contract address
}

// LoadPoolAddresses loads pool addresses using standard configuration hierarchy
// factoryAddress takes precedence over individual pool addresses for modern factory-based configuration
func LoadPoolAddresses(factoryAddress string, logger *zap.Logger) (*PoolAddresses, error) {

	// Factory address validation
	if factoryAddress == "" {
		return nil, fmt.Errorf("factory address not configured: --ccqFactoryAddress must be specified for staking-based CCQ")
	}

	if !common.IsHexAddress(factoryAddress) {
		return nil, fmt.Errorf("invalid factory address: %s", factoryAddress)
	}

	// Factory-based configuration (modern approach)
	addresses := &PoolAddresses{
		FactoryAddress: common.HexToAddress(factoryAddress),
	}

	logger.Info("loaded factory-based staking configuration",
		zap.String("factory", addresses.FactoryAddress.Hex()))

	return addresses, nil
}
