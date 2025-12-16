package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/celestia-app/v6/metrics/server"
)

// Client is a client for the metrics registry service.
type Client struct {
	conn   *grpc.ClientConn
	client server.RegistryClient
}

// New creates a new metrics client connected to the given address.
func New(address string) (*Client, error) {
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		client: server.NewRegistryClient(conn),
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Register registers a node with the metrics server.
func (c *Client) Register(ctx context.Context, nodeID, address string, labels map[string]string) error {
	resp, err := c.client.Register(ctx, &server.RegisterRequest{
		NodeId:  nodeID,
		Address: address,
		Labels:  labels,
	})
	if err != nil {
		return fmt.Errorf("failed to register: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("registration failed: %s", resp.Message)
	}
	return nil
}

// Deregister removes a node from the metrics server.
func (c *Client) Deregister(ctx context.Context, nodeID string) error {
	resp, err := c.client.Deregister(ctx, &server.DeregisterRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return fmt.Errorf("failed to deregister: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("node %s not found", nodeID)
	}
	return nil
}

// Target represents a registered target.
type Target struct {
	NodeID       string
	Address      string
	Labels       map[string]string
	RegisteredAt time.Time
}

// ListTargets returns all registered targets.
func (c *Client) ListTargets(ctx context.Context) ([]Target, error) {
	resp, err := c.client.ListTargets(ctx, &server.ListTargetsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list targets: %w", err)
	}

	targets := make([]Target, 0, len(resp.Targets))
	for _, t := range resp.Targets {
		targets = append(targets, Target{
			NodeID:       t.NodeId,
			Address:      t.Address,
			Labels:       t.Labels,
			RegisteredAt: time.Unix(t.RegisteredAt, 0),
		})
	}
	return targets, nil
}
