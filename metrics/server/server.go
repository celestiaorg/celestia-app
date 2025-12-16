package server

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Server is the metrics gRPC server.
type Server struct {
	UnimplementedRegistryServer

	registry *Registry
	writer   *FileSDWriter
	grpc     *grpc.Server
	port     int
}

// NewServer creates a new metrics server.
func NewServer(port int, targetsFile string) *Server {
	writer := NewFileSDWriter(targetsFile)

	s := &Server{
		writer: writer,
		port:   port,
	}

	// Create registry with callback to write file on changes
	s.registry = NewRegistry(func() {
		if err := writer.Write(s.registry.List()); err != nil {
			log.Printf("failed to write targets file: %v", err)
		}
	})

	return s
}

// Start starts the gRPC server.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.grpc = grpc.NewServer()
	RegisterRegistryServer(s.grpc, s)
	reflection.Register(s.grpc)

	// Write initial empty targets file
	if err := s.writer.Write(s.registry.List()); err != nil {
		log.Printf("warning: failed to write initial targets file: %v", err)
	}

	log.Printf("metrics server listening on port %d", s.port)
	log.Printf("writing targets to %s", s.writer.Path())

	return s.grpc.Serve(lis)
}

// Stop gracefully stops the server.
func (s *Server) Stop() {
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
}

// Register implements RegistryServer.
func (s *Server) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	if req.NodeId == "" {
		return &RegisterResponse{
			Success: false,
			Message: "node_id is required",
		}, nil
	}
	if req.Address == "" {
		return &RegisterResponse{
			Success: false,
			Message: "address is required",
		}, nil
	}

	s.registry.Register(req.NodeId, req.Address, req.Labels)

	log.Printf("registered node %s at %s", req.NodeId, req.Address)

	return &RegisterResponse{
		Success: true,
		Message: fmt.Sprintf("registered %s", req.NodeId),
	}, nil
}

// Deregister implements RegistryServer.
func (s *Server) Deregister(ctx context.Context, req *DeregisterRequest) (*DeregisterResponse, error) {
	if req.NodeId == "" {
		return &DeregisterResponse{Success: false}, nil
	}

	found := s.registry.Deregister(req.NodeId)
	if found {
		log.Printf("deregistered node %s", req.NodeId)
	}

	return &DeregisterResponse{Success: found}, nil
}

// ListTargets implements RegistryServer.
func (s *Server) ListTargets(ctx context.Context, req *ListTargetsRequest) (*ListTargetsResponse, error) {
	entries := s.registry.List()

	resp := &ListTargetsResponse{
		Targets: make([]*Target, 0, len(entries)),
	}

	for _, e := range entries {
		resp.Targets = append(resp.Targets, &Target{
			NodeId:       e.NodeID,
			Address:      e.Address,
			Labels:       e.Labels,
			RegisteredAt: e.RegisteredAt.Unix(),
		})
	}

	return resp, nil
}

// LoadInitialTargets loads targets from a JSON file.
func (s *Server) LoadInitialTargets(filePath string) error {
	// This can be implemented to load initial targets from a config file
	// For now, we start with an empty registry
	return nil
}
