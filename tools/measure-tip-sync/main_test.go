package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestNewTOFUHostKeyCallback(t *testing.T) {
	// Generate two different key pairs
	pub1, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key 1: %v", err)
	}
	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key 2: %v", err)
	}
	sshPub1, err := ssh.NewPublicKey(pub1)
	if err != nil {
		t.Fatalf("creating SSH public key 1: %v", err)
	}
	sshPub2, err := ssh.NewPublicKey(pub2)
	if err != nil {
		t.Fatalf("creating SSH public key 2: %v", err)
	}

	callback := newTOFUHostKeyCallback()
	addr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}

	// First connection to a host should be accepted
	err = callback("host1:22", addr, sshPub1)
	if err != nil {
		t.Fatalf("first connection should be accepted: %v", err)
	}

	// Subsequent connection with the same key should be accepted
	err = callback("host1:22", addr, sshPub1)
	if err != nil {
		t.Fatalf("same key should be accepted: %v", err)
	}

	// Connection with a different key to the same host should be rejected
	err = callback("host1:22", addr, sshPub2)
	if err == nil {
		t.Fatal("different key to same host should be rejected")
	}

	// Different host with a different key should be accepted
	err = callback("host2:22", addr, sshPub2)
	if err != nil {
		t.Fatalf("different host should be accepted: %v", err)
	}
}
