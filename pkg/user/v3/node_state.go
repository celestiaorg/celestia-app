package v3

import (
	"time"

	"google.golang.org/grpc"
)

// NodeStatus represents the state of a node connection.
type NodeStatus int

const (
	// NodeActive means the node is healthy and accepting submissions.
	NodeActive NodeStatus = iota
	// NodeRecovering means the node had a transient error and is backing off.
	NodeRecovering
	// NodeStopped means the node had a terminal failure.
	NodeStopped
)

const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
	maxFailures    = 10
)

// NodeConnection tracks per-node state for the submission pipeline.
type NodeConnection struct {
	id            int
	conn          *grpc.ClientConn
	status        NodeStatus
	retryAfter    time.Time
	lastSubmitted uint64 // highest sequence submitted on this node
	failures      int
	stopErr       error
}

// NewNodeConnection creates a new NodeConnection in Active state.
func NewNodeConnection(id int, conn *grpc.ClientConn) *NodeConnection {
	return &NodeConnection{
		id:     id,
		conn:   conn,
		status: NodeActive,
	}
}

// IsAvailable returns true if the node is active or has recovered from a
// transient error (past its retry backoff time).
func (n *NodeConnection) IsAvailable() bool {
	switch n.status {
	case NodeActive:
		return true
	case NodeRecovering:
		if time.Now().After(n.retryAfter) {
			n.status = NodeActive
			return true
		}
		return false
	default:
		return false
	}
}

// MarkRecovering transitions the node to Recovering with exponential backoff.
func (n *NodeConnection) MarkRecovering() {
	n.failures++
	if n.failures >= maxFailures {
		n.status = NodeStopped
		return
	}
	n.status = NodeRecovering
	backoff := min(initialBackoff*(1<<(n.failures-1)), maxBackoff)
	n.retryAfter = time.Now().Add(backoff)
}

// MarkStopped transitions the node to Stopped with the given error.
func (n *NodeConnection) MarkStopped(err error) {
	n.status = NodeStopped
	n.stopErr = err
}

// ResetFailures clears the failure counter on a successful operation.
func (n *NodeConnection) ResetFailures() {
	n.failures = 0
}

// NeedsSubmission returns true if this node hasn't submitted the given sequence yet.
func (n *NodeConnection) NeedsSubmission(seq uint64) bool {
	return seq > n.lastSubmitted
}

// RecordSubmission marks a sequence as submitted on this node.
func (n *NodeConnection) RecordSubmission(seq uint64) {
	if seq > n.lastSubmitted {
		n.lastSubmitted = seq
	}
}
