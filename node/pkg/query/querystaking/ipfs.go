package querystaking

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// IPFS-related metrics
var (
	ipfsFetchErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ccq_ipfs_fetch_errors_total",
			Help: "Total number of IPFS fetch errors by error type",
		}, []string{"error_type"})

	ipfsCacheHitRate = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ccq_ipfs_cache_total",
			Help: "Total number of IPFS cache lookups by result",
		}, []string{"result"})
)

// IPFSClient handles fetching and caching conversion tables from IPFS
type IPFSClient struct {
	httpClient *http.Client
	gateway    string
	cache      *sync.Map // CID string -> *ConversionTable
	logger     *zap.Logger
}

// NewIPFSClient creates a new IPFS client
func NewIPFSClient(gateway string, timeout time.Duration, logger *zap.Logger) *IPFSClient {
	return &IPFSClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		gateway: gateway,
		cache:   &sync.Map{},
		logger:  logger.With(zap.String("component", "ipfs-client")),
	}
}

// FetchConversionTable fetches and parses a conversion table from IPFS
// Returns cached value if available, otherwise fetches from IPFS gateway
func (c *IPFSClient) FetchConversionTable(ctx context.Context, cidBytes [32]byte) (*ConversionTable, error) {
	// Convert bytes32 to CID string
	cidStr, err := bytes32ToCIDString(cidBytes)
	if err != nil {
		ipfsFetchErrors.WithLabelValues("cid_parse").Inc()
		return nil, fmt.Errorf("failed to parse CID from bytes32: %w", err)
	}

	// Check cache first
	if cached, ok := c.cache.Load(cidStr); ok {
		ipfsCacheHitRate.WithLabelValues("hit").Inc()
		c.logger.Debug("cache hit for conversion table", zap.String("cid", cidStr))
		return cached.(*ConversionTable), nil
	}
	ipfsCacheHitRate.WithLabelValues("miss").Inc()

	// Fetch from IPFS
	c.logger.Debug("fetching conversion table from IPFS", zap.String("cid", cidStr))
	conversionTable, err := c.fetchFromIPFS(ctx, cidStr)
	if err != nil {
		// Check cache again in case of network error (stale cache is better than no data)
		if cached, ok := c.cache.Load(cidStr); ok {
			c.logger.Warn("IPFS fetch failed, using stale cache", zap.String("cid", cidStr), zap.Error(err))
			return cached.(*ConversionTable), nil
		}
		return nil, err
	}

	// Store in cache
	c.cache.Store(cidStr, conversionTable)

	return conversionTable, nil
}

// fetchFromIPFS performs the HTTP GET request to the IPFS gateway
func (c *IPFSClient) fetchFromIPFS(ctx context.Context, cidStr string) (*ConversionTable, error) {
	url := c.gateway + cidStr

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		ipfsFetchErrors.WithLabelValues("request_creation").Inc()
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		ipfsFetchErrors.WithLabelValues("network").Inc()
		c.logger.Error("IPFS HTTP request failed",
			zap.String("url", url),
			zap.Error(err))
		return nil, fmt.Errorf("failed to fetch from IPFS gateway: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ipfsFetchErrors.WithLabelValues("http_status").Inc()
		c.logger.Error("IPFS gateway returned non-200 status",
			zap.String("url", url),
			zap.Int("status", resp.StatusCode))
		return nil, fmt.Errorf("IPFS gateway returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ipfsFetchErrors.WithLabelValues("read_body").Inc()
		return nil, fmt.Errorf("failed to read IPFS response body: %w", err)
	}

	// Parse JSON
	var conversionTable ConversionTable
	if err := json.Unmarshal(body, &conversionTable); err != nil {
		ipfsFetchErrors.WithLabelValues("json_parse").Inc()
		c.logger.Error("failed to parse conversion table JSON",
			zap.String("cid", cidStr),
			zap.Error(err),
			zap.String("body", string(body)))
		return nil, fmt.Errorf("failed to parse conversion table JSON: %w", err)
	}

	c.logger.Info("successfully fetched conversion table from IPFS",
		zap.String("cid", cidStr),
		zap.String("url", url))

	return &conversionTable, nil
}

// bytes32ToCIDString converts a 32-byte hash digest to a CIDv1 base32 string
// The bytes32 contains the raw sha256 hash digest
func bytes32ToCIDString(hashBytes [32]byte) (string, error) {
	// Create a multihash from the sha256 digest
	mh, err := multihash.Encode(hashBytes[:], multihash.SHA2_256)
	if err != nil {
		return "", fmt.Errorf("failed to encode multihash: %w", err)
	}

	// Create CIDv1 with raw codec
	c := cid.NewCidV1(cid.Raw, mh)

	// Encode as base32 (default for CIDv1)
	return c.String(), nil
}
