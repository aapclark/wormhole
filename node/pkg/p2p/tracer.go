package p2p

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var _ = pubsub.RawTracer(gossipTracer{})

var (
	p2pDrop = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "wormhole_p2p_drops",
			Help: "Total number of messages that were dropped by libp2p",
		})
	p2pPublish = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "wormhole_p2p_publishes",
			Help: "Total number of messages that were published by libp2p",
		})
	p2pMessageDuplicate = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wormhole_p2p_duplicates",
			Help: "Total number of duplicate messages that were detected by libp2p",
		}, []string{"topic"})
	p2pMessageUndeliverable = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "wormhole_p2p_undeliverable",
			Help: "Total number of messages undeliverable by libp2p",
		}, []string{"topic"})
	p2pPeerThrottled = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_p2p_peer_throttled",
			Help: "Peer throttled by libp2p",
		}, []string{"peer"})
	p2pTopics = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_p2p_topics",
			Help: "Current active pubsub topics",
		}, []string{"topic"})
	p2pMessageValidate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_p2p_message_validate",
			Help: "Message validated by libp2p",
		}, []string{"topic"})
	p2pMessageDeliver = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_p2p_message_deliver",
			Help: "Message delivered by libp2p",
		}, []string{"topic"})
	p2pMessageReject = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "wormhole_p2p_message_reject",
			Help: "Message rejected by libp2p",
		}, []string{"topic", "reason"})
	p2pTopicsGraft = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_topic_graft_totals",
		Help: "Total number of times peers were grafted to a topic",
	}, []string{"topic"})
	p2pTopicsPrune = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_topic_prune_totals",
		Help: "Total number of times peers were pruned from a topic",
	}, []string{"topic"})
	p2pRPCRecv = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_recv_total",
		Help: "The number of messages received via rpc for a particular control message",
	}, []string{"control_message"})
	p2pRPCSubRecv = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_recv_sub_total",
		Help: "The number of subscription messages received via rpc",
	})
	p2pRPCPubRecv = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_recv_pub_total",
		Help: "The number of publish messages received via rpc for a particular topic",
	}, []string{"topic"})
	p2pRPCDrop = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_drop_total",
		Help: "The number of messages dropped via rpc for a particular control message",
	}, []string{"control_message"})
	p2pRPCPubDrop = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_drop_pub_total",
		Help: "The number of publish messages dropped via rpc for a particular topic",
	}, []string{"topic"})
	p2pRPCSet = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_sent_total",
		Help: "The number of messages sent via rpc for a particular control message",
	}, []string{"control_message"})
	p2pRPCSubSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_sent_sub_total",
		Help: "The number of subscription messages sent via rpc",
	})
	p2pRPCPubSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_sent_pub_total",
		Help: "The number of publish messages sent via rpc for a particular topic",
	}, []string{"topic"})
	p2pRPCSubDrop = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wormhole_p2p_pubsub_rpc_drop_sub_total",
		Help: "The number of subscription messages dropped via rpc",
	})
)

// Initializes the values for the pubsub rpc action.
type action int

const (
	recv action = iota
	send
	drop
)

// This tracer is used to implement metrics collection for messages received
// and broadcasted through gossipsub.
type gossipTracer struct {
	host host.Host
}

// AddPeer .
func (g gossipTracer) AddPeer(p peer.ID, proto protocol.ID) {
	// no-op
}

// RemovePeer .
func (g gossipTracer) RemovePeer(p peer.ID) {
	// no-op
}

// Join .
func (g gossipTracer) Join(topic string) {
	p2pTopics.WithLabelValues(topic).Set(1)
}

// Leave .
func (g gossipTracer) Leave(topic string) {
	p2pTopics.WithLabelValues(topic).Set(0)
}

// Graft .
func (g gossipTracer) Graft(p peer.ID, topic string) {
	p2pTopicsGraft.WithLabelValues(topic).Inc()
}

// Prune .
func (g gossipTracer) Prune(p peer.ID, topic string) {
	p2pTopicsPrune.WithLabelValues(topic).Inc()
}

// ValidateMessage .
func (g gossipTracer) ValidateMessage(msg *pubsub.Message) {
	p2pMessageValidate.WithLabelValues(*msg.Topic).Inc()
}

// DeliverMessage .
func (g gossipTracer) DeliverMessage(msg *pubsub.Message) {
	p2pMessageDeliver.WithLabelValues(*msg.Topic).Inc()
}

// RejectMessage .
func (g gossipTracer) RejectMessage(msg *pubsub.Message, reason string) {
	p2pMessageReject.WithLabelValues(*msg.Topic, reason).Inc()
}

// DuplicateMessage .
func (g gossipTracer) DuplicateMessage(msg *pubsub.Message) {
	p2pMessageDuplicate.WithLabelValues(*msg.Topic).Inc()
}

// UndeliverableMessage .
func (g gossipTracer) UndeliverableMessage(msg *pubsub.Message) {
	p2pMessageUndeliverable.WithLabelValues(*msg.Topic).Inc()
}

// ThrottlePeer increments the throttled counter for provided peer ID
func (g gossipTracer) ThrottlePeer(p peer.ID) {
	p2pPeerThrottled.WithLabelValues(p.ShortString()).Inc()
}

// RecvRPC .
func (g gossipTracer) RecvRPC(rpc *pubsub.RPC) {
	g.setMetricFromRPC(recv, p2pRPCSubRecv, p2pRPCPubRecv, p2pRPCRecv, rpc)
}

// SendRPC .
func (g gossipTracer) SendRPC(rpc *pubsub.RPC, p peer.ID) {
	g.setMetricFromRPC(send, p2pRPCSubSent, p2pRPCPubSent, p2pRPCSet, rpc)
}

// DropRPC .
func (g gossipTracer) DropRPC(rpc *pubsub.RPC, p peer.ID) {
	g.setMetricFromRPC(drop, p2pRPCSubDrop, p2pRPCPubDrop, p2pRPCDrop, rpc)
}

func (g gossipTracer) setMetricFromRPC(act action, subCtr prometheus.Counter, pubCtr, ctrlCtr *prometheus.CounterVec, rpc *pubsub.RPC) {
	subCtr.Add(float64(len(rpc.Subscriptions)))
	if rpc.Control != nil {
		ctrlCtr.WithLabelValues("graft").Add(float64(len(rpc.Control.Graft)))
		ctrlCtr.WithLabelValues("prune").Add(float64(len(rpc.Control.Prune)))
		ctrlCtr.WithLabelValues("ihave").Add(float64(len(rpc.Control.Ihave)))
		ctrlCtr.WithLabelValues("iwant").Add(float64(len(rpc.Control.Iwant)))
		ctrlCtr.WithLabelValues("idontwant").Add(float64(len(rpc.Control.Idontwant)))
	}
	for _, msg := range rpc.Publish {
		// For incoming messages from pubsub, we do not record metrics for them as these values
		// could be junk.
		if act == recv {
			continue
		}
		pubCtr.WithLabelValues(*msg.Topic).Inc()
	}
}
