import {
  beforeAll,
  describe,
  expect,
  jest,
  test,
} from "@jest/globals";
import axios from "axios";
import { type Address, parseEther, toBytes } from "viem";
import { privateKeyToAccount, generatePrivateKey } from "viem/accounts";
import {
  EthCallQueryRequest,
  PerChainQueryRequest,
  QueryRequest,
  QueryResponse,
  sign,
} from "../../src";
import {
  createClient,
  createTestEthCallData,
  EVM_QUERY_TYPE,
  getPoolAddress,
  mintAndTransferTokens,
  QUERY_URL,
  setupAxiosInterceptor,
  STAKING_FACTORY_ADDRESS,
  W_TOKEN_ADDRESS,
  WETH_ADDRESS,
  ERC20_ABI,
  POOL_STAKE_ABI,
} from "./test-utils";

jest.setTimeout(120000);
setupAxiosInterceptor();

const ENV = "DEVNET";
const STAKE_AMOUNT = "50000";

// Test wallet
let testWallet: { privateKey: `0x${string}`; address: Address };
let poolAddress: Address;

beforeAll(async () => {
  console.log("\nSetting up prefixed signature test wallet...");

  // Generate wallet
  const privateKey = generatePrivateKey();
  const account = privateKeyToAccount(privateKey);
  testWallet = { privateKey, address: account.address };

  // Get pool address
  poolAddress = await getPoolAddress(STAKING_FACTORY_ADDRESS, EVM_QUERY_TYPE);
  console.log("  Pool address:", poolAddress);

  // Fund wallet with ETH
  const minterClient = createClient();
  const ethHash = await minterClient.sendTransaction({
    to: testWallet.address,
    value: parseEther("1"),
  } as any);
  await minterClient.waitForTransactionReceipt({ hash: ethHash });

  // Mint tokens
  await mintAndTransferTokens(testWallet.address, STAKE_AMOUNT);

  // Approve and stake
  const walletClient = createClient(testWallet.privateKey);
  const stakeAmountWei = parseEther(STAKE_AMOUNT);

  const approveHash = await walletClient.writeContract({
    address: W_TOKEN_ADDRESS,
    abi: ERC20_ABI,
    functionName: "approve",
    args: [poolAddress, stakeAmountWei],
  } as any);
  await walletClient.waitForTransactionReceipt({ hash: approveHash });

  const stakeHash = await walletClient.writeContract({
    address: poolAddress,
    abi: POOL_STAKE_ABI,
    functionName: "stake",
    args: [stakeAmountWei],
  } as any);
  await walletClient.waitForTransactionReceipt({ hash: stakeHash });

  console.log("  Test wallet ready:", testWallet.address);
}, 60000);

describe("Prefixed Signature (EIP-191)", () => {
  test("query with prefixed signature succeeds", async () => {
    const client = createClient(testWallet.privateKey);

    // Build query
    const nameCallData = createTestEthCallData(WETH_ADDRESS, "name", "string");
    const blockNumber = await client.getBlockNumber();

    const ethCall = new EthCallQueryRequest(Number(blockNumber), [nameCallData]);
    const ethQuery = new PerChainQueryRequest(2, ethCall);
    const request = new QueryRequest(1, [ethQuery], undefined, testWallet.address);

    // Serialize and compute digest
    const serialized = request.serialize();
    const digest = QueryRequest.digest(ENV, serialized);

    // Sign with personal_sign (EIP-191 prefixed)
    // viem's signMessage adds the "\x19Ethereum Signed Message:\n" prefix
    const signature = await client.signMessage({
      message: { raw: toBytes(digest) },
    });

    // Submit with X-Signature-Format header
    const response = await axios.post(
      QUERY_URL,
      {
        bytes: Buffer.from(serialized).toString("hex"),
        signature: signature.slice(2), // Remove 0x prefix
      },
      {
        headers: {
          "Content-Type": "application/json",
          "X-Signature-Format": "eip191",
        },
      }
    );

    expect(response.status).toBe(200);
    expect(response.data.bytes).toBeTruthy();

    // Verify we can parse the response
    const queryResponse = QueryResponse.from(
      Buffer.from(response.data.bytes, "hex")
    );
    expect(queryResponse.responses.length).toBe(1);
  });

  test("raw signature still works (no header)", async () => {
    // Build query
    const nameCallData = createTestEthCallData(WETH_ADDRESS, "name", "string");
    const client = createClient();
    const blockNumber = await client.getBlockNumber();

    const ethCall = new EthCallQueryRequest(Number(blockNumber), [nameCallData]);
    const ethQuery = new PerChainQueryRequest(2, ethCall);
    const request = new QueryRequest(2, [ethQuery], undefined, testWallet.address);

    // Serialize and sign with raw ECDSA (using the sign helper)
    const serialized = request.serialize();
    const digest = QueryRequest.digest(ENV, serialized);
    const signature = sign(testWallet.privateKey.slice(2), digest);

    // Submit WITHOUT X-Signature-Format header (default = raw)
    const response = await axios.post(QUERY_URL, {
      bytes: Buffer.from(serialized).toString("hex"),
      signature,
    });

    expect(response.status).toBe(200);
    expect(response.data.bytes).toBeTruthy();
  });

  test("prefixed signature fails without header (wrong address recovered)", async () => {
    const client = createClient(testWallet.privateKey);

    // Build query
    const nameCallData = createTestEthCallData(WETH_ADDRESS, "name", "string");
    const blockNumber = await client.getBlockNumber();

    const ethCall = new EthCallQueryRequest(Number(blockNumber), [nameCallData]);
    const ethQuery = new PerChainQueryRequest(2, ethCall);
    const request = new QueryRequest(3, [ethQuery], undefined, testWallet.address);

    // Serialize and compute digest
    const serialized = request.serialize();
    const digest = QueryRequest.digest(ENV, serialized);

    // Sign with personal_sign (EIP-191 prefixed)
    const signature = await client.signMessage({
      message: { raw: toBytes(digest) },
    });

    // Submit WITHOUT the header - server will try raw recovery, get wrong address
    try {
      await axios.post(QUERY_URL, {
        bytes: Buffer.from(serialized).toString("hex"),
        signature: signature.slice(2),
      });
      // Should not reach here
      expect(true).toBe(false);
    } catch (error: any) {
      // Should fail because wrong address is recovered -> no stake found
      expect(error.response?.status).toBe(403);
      expect(error.response?.data).toContain("insufficient stake");
    }
  });
});
