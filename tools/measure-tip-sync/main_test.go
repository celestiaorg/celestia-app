package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestTOFUHostKeyStore(t *testing.T) {
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

	addr := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 22}

	t.Run("before confirm, accepts key changes", func(t *testing.T) {
		store := newTOFUHostKeyStore()

		// First key should be accepted
		if err := store.callback("host1:22", addr, sshPub1); err != nil {
			t.Fatalf("first key should be accepted: %v", err)
		}

		// Different key to same host should be accepted before confirm
		if err := store.callback("host1:22", addr, sshPub2); err != nil {
			t.Fatalf("different key should be accepted before confirm: %v", err)
		}
	})

	t.Run("after confirm, rejects key changes", func(t *testing.T) {
		store := newTOFUHostKeyStore()

		// Store key and confirm
		if err := store.callback("host1:22", addr, sshPub1); err != nil {
			t.Fatalf("first key should be accepted: %v", err)
		}
		store.Confirm()

		// Same key should still be accepted
		if err := store.callback("host1:22", addr, sshPub1); err != nil {
			t.Fatalf("same key should be accepted after confirm: %v", err)
		}

		// Different key should be rejected
		if err := store.callback("host1:22", addr, sshPub2); err == nil {
			t.Fatal("different key should be rejected after confirm")
		}
	})

	t.Run("after confirm, new hosts still accepted", func(t *testing.T) {
		store := newTOFUHostKeyStore()

		if err := store.callback("host1:22", addr, sshPub1); err != nil {
			t.Fatalf("first key should be accepted: %v", err)
		}
		store.Confirm()

		// New host should still be accepted (TOFU)
		if err := store.callback("host2:22", addr, sshPub2); err != nil {
			t.Fatalf("new host should be accepted after confirm: %v", err)
		}
	})
}
