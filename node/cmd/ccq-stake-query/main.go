package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/certusone/wormhole/node/pkg/query/querystaking"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
)

// Use types from querystaking package

func PackQueryTypePoolsCall(queryType [32]byte) []byte {
	// Calculate function selector for queryTypePools(bytes32)
	selector := crypto.Keccak256([]byte("queryTypePools(bytes32)"))[:4]

	result := make([]byte, 4+32)
	copy(result[0:4], selector)
	copy(result[4:36], queryType[:])

	return result
}

// StakeInfo methods now available through querystaking.StakeInfo

type Client struct {
	ethClient *ethclient.Client
	rpcClient *rpc.Client
}

func NewClient(rpcURL string) (*Client, error) {
	rpcClient, err := rpc.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	ethClient := ethclient.NewClient(rpcClient)

	return &Client{
		ethClient: ethClient,
		rpcClient: rpcClient,
	}, nil
}

func (c *Client) Close() {
	c.rpcClient.Close()
}

func (c *Client) GetPoolAddress(factoryAddress common.Address, queryType [32]byte) (common.Address, error) {
	callData := PackQueryTypePoolsCall(queryType)

	result, err := c.ethClient.CallContract(context.Background(), ethereum.CallMsg{
		To:   &factoryAddress,
		Data: callData,
	}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to call queryTypePools: %w", err)
	}

	if len(result) != 32 {
		return common.Address{}, fmt.Errorf("unexpected result length: got %d want 32", len(result))
	}

	// Extract address from the last 20 bytes
	return common.BytesToAddress(result[12:32]), nil
}

func (c *Client) GetStakeInfo(poolAddress, stakerAddress common.Address) (*querystaking.StakeInfo, error) {
	callData := querystaking.PackStakesCall(stakerAddress)

	result, err := c.ethClient.CallContract(context.Background(), ethereum.CallMsg{
		To:   &poolAddress,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call stakes: %w", err)
	}

	fmt.Printf("result: %v, err: %v", result, err)

	return querystaking.ParseStakeInfo(result)
}

func formatTimestamp(ts uint64) string {
	if ts == 0 {
		return "not set"
	}
	return time.Unix(int64(ts), 0).Format("2006-01-02 15:04:05 UTC")
}

func formatAmount(amount *uint256.Int) string {
	if amount == nil || amount.Cmp(uint256.NewInt(0)) == 0 {
		return "0"
	}

	// Convert to big.Int for easier formatting
	bigIntAmount := amount.ToBig()

	// Assuming 18 decimals (adjust based on your token)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	quotient := new(big.Int).Div(bigIntAmount, divisor)
	remainder := new(big.Int).Mod(bigIntAmount, divisor)

	if remainder.Cmp(big.NewInt(0)) == 0 {
		return quotient.String()
	}

	// Format with decimals
	return fmt.Sprintf("%s.%018s", quotient.String(), remainder.String())
}

func main() {
	var (
		rpcURL       = flag.String("rpc", "", "RPC URL")
		factoryAddr  = flag.String("factory", "", "Factory contract address")
		poolAddr     = flag.String("pool", "", "Pool contract address (optional, will query factory if not provided)")
		stakerAddr   = flag.String("staker", "", "Staker address to query")
		queryTypeHex = flag.String("querytype", "", "Query type as hex string (required if using factory)")
	)
	flag.Parse()

	if *stakerAddr == "" {
		log.Fatal("staker address is required")
	}

	if *poolAddr == "" && (*factoryAddr == "" || *queryTypeHex == "") {
		log.Fatal("either pool address OR (factory address AND query type) must be provided")
	}

	// Create client
	client, err := NewClient(*rpcURL)
	if err != nil {
		log.Fatal("Failed to create client:", err)
	}
	defer client.Close()

	stakerAddress := common.HexToAddress(*stakerAddr)
	var poolAddress common.Address

	// Get pool address from factory if not provided directly
	if *poolAddr == "" {
		factoryAddress := common.HexToAddress(*factoryAddr)

		// Parse query type
		queryTypeBytes := common.HexToHash(*queryTypeHex)
		var queryType [32]byte
		copy(queryType[:], queryTypeBytes[:])

		fmt.Printf("🏭 Querying factory at %s for query type %s\n", factoryAddress.Hex(), *queryTypeHex)

		poolAddress, err = client.GetPoolAddress(factoryAddress, queryType)
		if err != nil {
			log.Fatal("Failed to get pool address:", err)
		}

		if poolAddress == (common.Address{}) {
			log.Fatal("No pool found for the given query type")
		}

		fmt.Printf("📍 Found pool at address: %s\n", poolAddress.Hex())
	} else {
		poolAddress = common.HexToAddress(*poolAddr)
		fmt.Printf("📍 Using provided pool address: %s\n", poolAddress.Hex())
	}

	// Get stake info
	fmt.Printf("👤 Querying stake info for staker: %s\n", stakerAddress.Hex())

	stakeInfo, err := client.GetStakeInfo(poolAddress, stakerAddress)
	log.Printf("stakeInfo: %v", stakeInfo)
	if err != nil {
		log.Fatal("Failed to get stake info:", err)
	}

	// Display results
	fmt.Printf("\n📊 Stake Information:\n")
	fmt.Printf("─────────────────────────────────────────────────\n")
	fmt.Printf("Amount Staked:        %s tokens\n", formatAmount(stakeInfo.Amount))
	// fmt.Printf("Capacity:             %s tokens\n", formatAmount(stakeInfo.Capacity))
	fmt.Printf("Conversion Index:     %s\n", stakeInfo.ConversionTableIndex.String())
	fmt.Printf("Lockup Ends:          %s\n", formatTimestamp(stakeInfo.LockupEnd))
	fmt.Printf("Access Ends:          %s\n", formatTimestamp(stakeInfo.AccessEnd))
	// fmt.Printf("Last Claimed:         %s\n", formatTimestamp(stakeInfo.LastClaimed))
	fmt.Printf("─────────────────────────────────────────────────\n")

	// Show status
	currentTime := uint64(time.Now().Unix())

	if !stakeInfo.HasStake() {
		fmt.Printf("❌ No stake found for this address\n")
	} else if stakeInfo.IsInLockup(currentTime) {
		fmt.Printf("🔒 Stake is currently locked (cannot withdraw)\n")
		timeLeft := stakeInfo.LockupEnd - currentTime
		fmt.Printf("⏰ Time until unlocked: %s\n", time.Duration(timeLeft)*time.Second)
	} else if stakeInfo.IsInAccessPeriod(currentTime) {
		fmt.Printf("✅ Stake is unlocked (can withdraw)\n")
		timeLeft := stakeInfo.AccessEnd - currentTime
		fmt.Printf("⏰ Time until access expires: %s\n", time.Duration(timeLeft)*time.Second)
	} else if stakeInfo.HasExpired(currentTime) {
		fmt.Printf("⚠️  Stake access period has expired\n")
	}
}
