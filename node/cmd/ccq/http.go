package ccq

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/certusone/wormhole/node/pkg/common"
	gossipv1 "github.com/certusone/wormhole/node/pkg/proto/gossip/v1"
	"github.com/certusone/wormhole/node/pkg/query"
	"github.com/certusone/wormhole/node/pkg/query/queryratelimit"
	eth_common "github.com/ethereum/go-ethereum/common"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/mux"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
)

const MAX_BODY_SIZE = 5 * 1024 * 1024

type queryRequest struct {
	Bytes     string `json:"bytes"`
	Signature string `json:"signature"`
}

type queryResponse struct {
	Bytes      string   `json:"bytes"`
	Signatures []string `json:"signatures"`
}

type httpServer struct {
	topic            *pubsub.Topic
	logger           *zap.Logger
	env              common.Environment
	permissions      *Permissions // nil when using staking-based rate limiting
	signerKey        *ecdsa.PrivateKey
	pendingResponses *PendingResponses
	loggingMap       *LoggingMap

	// Staking-based rate limiting (new)
	policyProvider *queryratelimit.PolicyProvider // nil when using legacy permissions or no staking
	limitEnforcer  *queryratelimit.Enforcer       // nil when using legacy permissions or no staking

	// Basic rate limiting for DoS protection (used when permissions is nil)
	basicRateLimitersMu sync.RWMutex
	basicRateLimiters   map[string]*rate.Limiter // Key is staker address
}

func (s *httpServer) handleQuery(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers for all requests.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Set CORS headers for the preflight request
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "PUT, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Api-Key")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	start := time.Now()
	allQueryRequestsReceived.Inc()

	// TODO: ??
	// Decode the body first. This is because the library seems to hang if we receive a large body and return without decoding it.
	// This could be a slight waste of resources, but should not be a DoS risk because we cap the max body size.

	var q queryRequest
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MAX_BODY_SIZE)).Decode(&q)
	if err != nil {
		s.logger.Error("failed to decode body", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		invalidQueryRequestReceived.WithLabelValues("failed_to_decode_body").Inc()
		return
	}

	queryRequestBytes, err := hex.DecodeString(q.Bytes)
	if err != nil {
		s.logger.Error("failed to decode request bytes", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		invalidQueryRequestReceived.WithLabelValues("failed_to_decode_request").Inc()
		return
	}

	signature, err := hex.DecodeString(q.Signature)
	if err != nil {
		s.logger.Error("failed to decode signature bytes", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		invalidQueryRequestReceived.WithLabelValues("failed_to_decode_signature").Inc()
		return
	}

	signedQueryRequest := &gossipv1.SignedQueryRequest{
		QueryRequest: queryRequestBytes,
		Signature:    signature,
	}

	var userIdentifier string // For logging and metrics
	var queryReq *query.QueryRequest

	// Two modes: with permissions (legacy) or without (staking-based)
	if s.permissions != nil {
		s.logger.Warn("not using permissions")
		// Legacy mode: API key-based permissions
		apiKeys, exists := r.Header["X-Api-Key"]
		if !exists || len(apiKeys) != 1 {
			s.logger.Error("received a request with the wrong number of api keys", zap.Stringer("url", r.URL), zap.Int("numApiKeys", len(apiKeys)))
			http.Error(w, "api key is missing", http.StatusUnauthorized)
			invalidQueryRequestReceived.WithLabelValues("missing_api_key").Inc()
			return
		}
		apiKey := strings.ToLower(apiKeys[0])

		permEntry, exists := s.permissions.GetUserEntry(apiKey)
		if !exists {
			s.logger.Error("invalid api key", zap.String("apiKey", apiKey))
			http.Error(w, "invalid api key", http.StatusForbidden)
			invalidQueryRequestReceived.WithLabelValues("invalid_api_key").Inc()
			return
		}

		if permEntry.rateLimiter != nil && !permEntry.rateLimiter.Allow() {
			s.logger.Debug("denying request due to rate limit", zap.String("userId", permEntry.userName))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			rateLimitExceededByUser.WithLabelValues(permEntry.userName).Inc()
			return
		}

		totalRequestsByUser.WithLabelValues(permEntry.userName).Inc()
		userIdentifier = permEntry.userName

		// Validate request with permissions (API key already validated above)
		status, qr, err := validateRequest(s.logger, s.env, permEntry, s.signerKey, signedQueryRequest)
		if err != nil {
			s.logger.Error("failed to validate request", zap.String("userId", userIdentifier), zap.String("requestId", hex.EncodeToString(signedQueryRequest.Signature)), zap.Int("status", status), zap.Error(err))
			http.Error(w, err.Error(), status)
			invalidRequestsByUser.WithLabelValues(userIdentifier).Inc()
			return
		}
		queryReq = qr
	} else {
		// New mode: Signature-based with basic DoS protection
		// Guardian nodes will enforce staking-based rate limits

		// Sign the request if it's unsigned and we have a signer key
		if len(signedQueryRequest.Signature) == 0 {
			if s.signerKey == nil {
				s.logger.Error("request not signed and no signer key configured")
				http.Error(w, "request must be signed", http.StatusBadRequest)
				invalidQueryRequestReceived.WithLabelValues("request_not_signed").Inc()
				return
			}

			digest := query.QueryRequestDigest(s.env, signedQueryRequest.QueryRequest)
			signedQueryRequest.Signature, err = ethCrypto.Sign(digest.Bytes(), s.signerKey)
			if err != nil {
				s.logger.Error("failed to sign request", zap.Error(err))
				http.Error(w, "failed to sign request", http.StatusInternalServerError)
				invalidQueryRequestReceived.WithLabelValues("failed_to_sign_request").Inc()
				return
			}
		}

		// Basic validation of query request structure
		var qr query.QueryRequest
		err = qr.Unmarshal(signedQueryRequest.QueryRequest)
		if err != nil {
			s.logger.Error("failed to unmarshal request", zap.Error(err))
			http.Error(w, "failed to unmarshal request", http.StatusBadRequest)
			invalidQueryRequestReceived.WithLabelValues("failed_to_unmarshal_request").Inc()
			return
		}

		if err := qr.Validate(); err != nil {
			s.logger.Error("invalid query request", zap.Error(err))
			http.Error(w, "invalid query request", http.StatusBadRequest)
			invalidQueryRequestReceived.WithLabelValues("failed_to_validate_request").Inc()
			return
		}

		// Recover signer address from signature
		digest := query.QueryRequestDigest(s.env, signedQueryRequest.QueryRequest)
		signerAddr, err := query.RecoverQueryRequestSigner(digest.Bytes(), signedQueryRequest.Signature)
		if err != nil {
			s.logger.Error("failed to recover signer from signature", zap.Error(err))
			http.Error(w, "invalid signature", http.StatusBadRequest)
			invalidQueryRequestReceived.WithLabelValues("failed_to_recover_signer").Inc()
			return
		}

		// Determine rate limit key: use staker address if provided, otherwise signer
		var rateLimitKey eth_common.Address
		if len(qr.StakerAddress) == 20 {
			rateLimitKey = eth_common.BytesToAddress(qr.StakerAddress)
			userIdentifier = "delegated:" + signerAddr.Hex() + "->staker:" + rateLimitKey.Hex()
			s.logger.Debug("delegated query", zap.String("signer", signerAddr.Hex()), zap.String("staker", rateLimitKey.Hex()))
		} else {
			rateLimitKey = signerAddr
			userIdentifier = "signer:" + signerAddr.Hex()
			s.logger.Debug("self-staking query", zap.String("signer", signerAddr.Hex()))
		}

		// If staking-based rate limiting is enabled, enforce it here
		if s.policyProvider != nil && s.limitEnforcer != nil {
			// Determine staker address (same as rateLimitKey above)
			stakerAddr := rateLimitKey

			// Fetch staking policy
			policy, err := s.policyProvider.GetPolicy(r.Context(), signerAddr, stakerAddr)
			if err != nil {
				s.logger.Error("failed to fetch staking policy",
					zap.String("signer", signerAddr.Hex()),
					zap.String("staker", stakerAddr.Hex()),
					zap.Error(err))
				http.Error(w, "failed to verify staking eligibility", http.StatusInternalServerError)
				invalidQueryRequestReceived.WithLabelValues("failed_to_fetch_policy").Inc()
				return
			}

			// Check if user has any limits (i.e., has stake)
			if len(policy.Limits.Types) == 0 {
				s.logger.Info("requestor has insufficient stake",
					zap.String("signer", signerAddr.Hex()),
					zap.String("staker", stakerAddr.Hex()))

				// Provide more specific error message for delegation scenarios
				var errorMsg string
				if signerAddr != stakerAddr {
					errorMsg = fmt.Sprintf("insufficient stake for CCQ access: signer %s is not authorized to use staker %s's rate limits (or staker has no stake)",
						signerAddr.Hex(), stakerAddr.Hex())
				} else {
					errorMsg = fmt.Sprintf("insufficient stake for CCQ access: address %s has no stake or is below minimum threshold", signerAddr.Hex())
				}

				http.Error(w, errorMsg, http.StatusForbidden)
				invalidQueryRequestReceived.WithLabelValues("insufficient_stake").Inc()
				return
			}

			// Build action for rate limit enforcement
			action := &queryratelimit.Action{
				Key:   stakerAddr,
				Time:  time.Now(),
				Types: make(map[uint8]int),
			}

			for _, pcq := range qr.PerChainQueries {
				action.Types[uint8(pcq.Query.Type())] += 1
			}

			// Enforce rate limits
			limitResult, err := s.limitEnforcer.EnforcePolicy(r.Context(), policy, action)
			if err != nil {
				s.logger.Error("failed to enforce rate limit",
					zap.String("signer", signerAddr.Hex()),
					zap.String("staker", stakerAddr.Hex()),
					zap.Error(err))
				http.Error(w, "failed to enforce rate limit", http.StatusInternalServerError)
				invalidQueryRequestReceived.WithLabelValues("failed_to_enforce_rate_limit").Inc()
				return
			}

			if !limitResult.Allowed {
				s.logger.Info("rate limit exceeded",
					zap.String("signer", signerAddr.Hex()),
					zap.String("staker", stakerAddr.Hex()),
					zap.Any("exceededTypes", limitResult.ExceededTypes))
				http.Error(w, fmt.Sprintf("rate limit exceeded for query types: %v", limitResult.ExceededTypes), http.StatusTooManyRequests)
				invalidQueryRequestReceived.WithLabelValues("rate_limit_exceeded").Inc()
				return
			}

			s.logger.Debug("rate limit check passed",
				zap.String("signer", signerAddr.Hex()),
				zap.String("staker", stakerAddr.Hex()))
		}

		// Apply basic rate limiting for DoS protection (keyed by staker address)
		limiter := s.getOrCreateBasicRateLimiter(rateLimitKey.Hex())
		if !limiter.Allow() {
			s.logger.Debug("denying request due to basic rate limit", zap.String("rateLimitKey", rateLimitKey.Hex()))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			invalidQueryRequestReceived.WithLabelValues("basic_rate_limit_exceeded").Inc()
			return
		}

		queryReq = &qr
	}

	requestId := hex.EncodeToString(signedQueryRequest.Signature)
	s.logger.Info("received request from client", zap.String("userId", userIdentifier), zap.String("requestId", requestId))

	m := gossipv1.GossipMessage{
		Message: &gossipv1.GossipMessage_SignedQueryRequest{
			SignedQueryRequest: signedQueryRequest,
		},
	}

	b, err := proto.Marshal(&m)
	if err != nil {
		s.logger.Error("failed to marshal gossip message", zap.String("userId", userIdentifier), zap.String("requestId", requestId), zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		invalidQueryRequestReceived.WithLabelValues("failed_to_marshal_gossip_msg").Inc()
		return
	}

	pendingResponse := NewPendingResponse(signedQueryRequest, userIdentifier, queryReq)
	added := s.pendingResponses.Add(pendingResponse)
	if !added {
		s.logger.Info("duplicate request", zap.String("userId", userIdentifier), zap.String("requestId", requestId))
		http.Error(w, "Duplicate request", http.StatusBadRequest)
		invalidQueryRequestReceived.WithLabelValues("duplicate_request").Inc()
		return
	}

	// Log responses if permissions mode is enabled and user requested it
	if s.permissions != nil {
		apiKey := strings.ToLower(r.Header.Get("X-Api-Key"))
		if permEntry, exists := s.permissions.GetUserEntry(apiKey); exists && permEntry.logResponses {
			s.loggingMap.AddRequest(requestId)
		}
	}

	s.logger.Info("posting request to gossip", zap.String("userId", userIdentifier), zap.String("requestId", requestId))
	err = s.topic.Publish(r.Context(), b)
	if err != nil {
		s.logger.Error("failed to publish gossip message", zap.String("userId", userIdentifier), zap.String("requestId", requestId), zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		invalidQueryRequestReceived.WithLabelValues("failed_to_publish_gossip_msg").Inc()
		s.pendingResponses.Remove(pendingResponse)
		return
	}

	// Wait for the response or timeout
outer:
	select {
	case <-time.After(query.RequestTimeout + 5*time.Second):
		maxMatchingResponses, outstandingResponses, quorum := pendingResponse.getStats()
		s.logger.Info("publishing time out to client",
			zap.String("userId", userIdentifier),
			zap.String("requestId", requestId),
			zap.Int("maxMatchingResponses", maxMatchingResponses),
			zap.Int("outstandingResponses", outstandingResponses),
			zap.Int("quorum", quorum),
		)
		http.Error(w, "Timed out waiting for response", http.StatusGatewayTimeout)
	case res := <-pendingResponse.ch:
		s.logger.Info("publishing response to client", zap.String("userId", userIdentifier), zap.String("requestId", requestId))
		resBytes, respMarshalErr := res.Response.Marshal()
		if respMarshalErr != nil {
			s.logger.Error("failed to marshal response", zap.String("userId", userIdentifier), zap.String("requestId", requestId), zap.Error(respMarshalErr))
			http.Error(w, respMarshalErr.Error(), http.StatusInternalServerError)
			invalidQueryRequestReceived.WithLabelValues("failed_to_marshal_response").Inc()
			break
		}
		// Signature indices must be ascending for on-chain verification
		sort.Slice(res.Signatures, func(i, j int) bool {
			return res.Signatures[i].Index < res.Signatures[j].Index
		})
		signatures := make([]string, 0, len(res.Signatures))
		for _, sig := range res.Signatures {
			if sig.Index > math.MaxUint8 {
				boundsErr := "Signature index out of bounds"
				s.logger.Error(boundsErr, zap.Int("sig.Index", sig.Index))
				http.Error(w, boundsErr, http.StatusInternalServerError)
				invalidQueryRequestReceived.WithLabelValues("failed_to_marshal_response").Inc()
				break outer
			}
			// ECDSA signature + a byte for the index of the guardian in the guardian set
			signature := fmt.Sprintf("%s%02x", sig.Signature, uint8(sig.Index)) // #nosec G115 -- This is validated above
			signatures = append(signatures, signature)
		}
		w.Header().Add("Content-Type", "application/json")
		encodeErr := json.NewEncoder(w).Encode(&queryResponse{
			Signatures: signatures,
			Bytes:      hex.EncodeToString(resBytes),
		})
		if encodeErr != nil {
			s.logger.Error("failed to encode response", zap.String("userId", userIdentifier), zap.String("requestId", requestId), zap.Error(encodeErr))
			http.Error(w, encodeErr.Error(), http.StatusInternalServerError)
			invalidQueryRequestReceived.WithLabelValues("failed_to_encode_response").Inc()
			break
		}
	case errEntry := <-pendingResponse.errCh:
		s.logger.Info("publishing error response to client", zap.String("userId", userIdentifier), zap.String("requestId", requestId), zap.Int("status", errEntry.status), zap.Error(errEntry.err))
		http.Error(w, errEntry.err.Error(), errEntry.status)
		// Metrics have already been pegged.
		break
	}

	totalQueryTime.Observe(float64(time.Since(start).Milliseconds()))
	validQueryRequestsReceived.Inc()
	s.pendingResponses.Remove(pendingResponse)
}

// getOrCreateBasicRateLimiter returns a rate limiter for the given signer address.
// Default: 10 requests per second with burst of 20 for basic DoS protection.
func (s *httpServer) getOrCreateBasicRateLimiter(signerAddr string) *rate.Limiter {
	s.basicRateLimitersMu.RLock()
	limiter, exists := s.basicRateLimiters[signerAddr]
	s.basicRateLimitersMu.RUnlock()

	if exists {
		return limiter
	}

	s.basicRateLimitersMu.Lock()
	defer s.basicRateLimitersMu.Unlock()

	// Check again in case another goroutine created it
	if limiter, exists := s.basicRateLimiters[signerAddr]; exists {
		return limiter
	}

	// Create new rate limiter: 10 requests/sec, burst of 20
	limiter = rate.NewLimiter(10, 20)
	s.basicRateLimiters[signerAddr] = limiter
	return limiter
}

func NewHTTPServer(addr string, t *pubsub.Topic, permissions *Permissions, signerKey *ecdsa.PrivateKey, p *PendingResponses, logger *zap.Logger, env common.Environment, loggingMap *LoggingMap, policyProvider *queryratelimit.PolicyProvider, limitEnforcer *queryratelimit.Enforcer) *http.Server {
	s := &httpServer{
		topic:             t,
		permissions:       permissions,
		signerKey:         signerKey,
		policyProvider:    policyProvider,
		limitEnforcer:     limitEnforcer,
		pendingResponses:  p,
		logger:            logger,
		env:               env,
		loggingMap:        loggingMap,
		basicRateLimiters: make(map[string]*rate.Limiter),
	}
	r := mux.NewRouter()
	r.HandleFunc("/v1/query", s.handleQuery).Methods("PUT", "POST", "OPTIONS")
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
