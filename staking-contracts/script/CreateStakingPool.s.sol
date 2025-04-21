// SPDX-License-Identifier: Apache-2.0
// slither-disable-start reentrancy-benign

pragma solidity 0.8.26;

import {Script} from "forge-std/Script.sol";
import {console} from "forge-std/console.sol";
import {QueryTypeStakerFactory} from "src/QueryTypeStakerFactory.sol";
import {QueryTypeStakingPool} from "src/QueryTypeStakingPool.sol";

contract CreateStakingPool is Script {
  // Default configuration constants matching ccq-rate-limits.json thresholds
  // EVM rates: 50000 = 1 QPS, 5000 = 1 QPM
  uint256 constant DEFAULT_STAKING_TOKEN_CAPACITY = 1_000_000 * 10**18; // 1 million tokens
  uint256 constant DEFAULT_MINIMUM_STAKE = 5_000 * 10**18; // 5,000 tokens (matches 1 QPM threshold)
  uint48 constant DEFAULT_LOCKUP_PERIOD = 900; // 900 seconds (15 minutes)
  uint48 constant DEFAULT_ACCESS_PERIOD = 1800; // 1800 seconds (30 minutes)

  function run() public returns (address) {
    // Get the deployer's private key and factory address from environment
    uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
    address factoryAddress = vm.envAddress("FACTORY_ADDRESS");

    // Get pool parameters from environment
    bytes32 queryType = vm.envBytes32("QUERY_TYPE");
    bytes32 initialEntry = vm.envBytes32("INITIAL_ENTRY");
    uint8 decayRate = uint8(vm.envUint("DECAY_RATE"));

    // Get configuration parameters from environment or use defaults
    uint256 stakingTokenCapacity = vm.envOr("STAKING_TOKEN_CAPACITY", DEFAULT_STAKING_TOKEN_CAPACITY);
    uint256 minimumStake = vm.envOr("MINIMUM_STAKE", DEFAULT_MINIMUM_STAKE);
    uint48 lockupPeriod = uint48(vm.envOr("LOCKUP_PERIOD", uint256(DEFAULT_LOCKUP_PERIOD)));
    uint48 accessPeriod = uint48(vm.envOr("ACCESS_PERIOD", uint256(DEFAULT_ACCESS_PERIOD)));

    // Start broadcasting transactions
    vm.startBroadcast(deployerPrivateKey);

    // Pool owner is the same as deployer
    address poolOwner = vm.addr(deployerPrivateKey);

    // Create the staking pool
    QueryTypeStakerFactory factory = QueryTypeStakerFactory(factoryAddress);
    address poolAddress = factory.createStakingPool(queryType, poolOwner, initialEntry, decayRate);

    console.log("========================================");
    console.log("Staking pool created at:", poolAddress);
    console.log("Configuring pool settings...");
    console.log("========================================");

    // Configure the pool
    QueryTypeStakingPool pool = QueryTypeStakingPool(poolAddress);

    // Set staking token capacity
    pool.setStakingTokenCapacity(stakingTokenCapacity);
    console.log("Staking token capacity set to:", stakingTokenCapacity / 10**18, "tokens");

    // Set minimum stake
    pool.setMinimumStake(minimumStake);
    console.log("Minimum stake set to:", minimumStake / 10**18, "tokens");

    // Set lockup period
    pool.setLockupPeriod(lockupPeriod);
    console.log("Lockup period set to:", lockupPeriod);
    console.log("  -> That's", lockupPeriod / 60, "minutes");

    // Set access period
    pool.setAccessPeriod(accessPeriod);
    console.log("Access period set to:", accessPeriod);
    console.log("  -> That's", accessPeriod / 60, "minutes");

    // Update conversion table with rate limits CID
    // The hash is computed at deploy time from the actual ccq-rate-limits.json file
    // This ensures the on-chain hash always matches what kubo/IPFS generates
    bytes32 rateLimitsCid = vm.envBytes32("RATE_LIMITS_CID");

    if (rateLimitsCid != bytes32(0)) {
      pool.updateConversionTable(rateLimitsCid);
      console.log("========================================");
      console.log("Updated conversion table with rate limits CID");
      console.log("Entry index:", pool.getConversionTableHistoryLength() - 1);
      console.log("========================================");
    }

    vm.stopBroadcast();

    console.log("========================================");
    console.log("Pool creation and configuration complete!");
    console.log("Pool address:", poolAddress);
    console.log("Configuration applied:");
    console.log("- Capacity: 1,000,000 tokens");
    console.log("- Min stake: 5,000 tokens (1 QPM threshold)");
    console.log("- Lockup: 15 minutes");
    console.log("- Access: 30 minutes");
    console.log("========================================");

    return poolAddress;
  }
}
