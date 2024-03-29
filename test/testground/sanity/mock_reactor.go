package sanity

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/conn"
	protomem "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

const (
	FirstChannel   = byte(0x01)
	SecondChannel  = byte(0x02)
	ThirdChannel   = byte(0x03)
	FourthChannel  = byte(0x04)
	FifthChannel   = byte(0x05)
	SixthChannel   = byte(0x06)
	SeventhChannel = byte(0x07)
	EighthChannel  = byte(0x08)
)

var priorities = make(map[byte]int)

func init() {
	for _, ch := range defaultTestChannels {
		priorities[ch.ID] = ch.Priority
	}
}

var defaultTestChannels = []*p2p.ChannelDescriptor{
	{
		ID:                  FirstChannel,
		Priority:            5,
		SendQueueCapacity:   2000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  SecondChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  ThirdChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  FourthChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  FifthChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  SixthChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  SeventhChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
	{
		ID:                  EighthChannel,
		Priority:            5,
		SendQueueCapacity:   1000,
		RecvBufferCapacity:  100,
		RecvMessageCapacity: 2000000,
		MessageType:         &protomem.Txs{},
	},
}

// MockReactor represents a mock implementation of the Reactor interface.
type MockReactor struct {
	p2p.BaseReactor
	channels []*conn.ChannelDescriptor
	peer     p2p.Peer

	mtx    sync.Mutex
	Traces []Trace
}

// NewMockReactor creates a new mock reactor.
func NewMockReactor(channels []*conn.ChannelDescriptor) *MockReactor {
	mr := &MockReactor{
		channels: channels,
	}
	mr.BaseReactor = *p2p.NewBaseReactor("MockReactor", mr)
	return mr
}

// GetChannels implements Reactor.
func (mr *MockReactor) GetChannels() []*conn.ChannelDescriptor {
	return mr.channels
}

// InitPeer implements Reactor.
func (mr *MockReactor) InitPeer(peer p2p.Peer) p2p.Peer {
	// Initialize any data structures related to the peer here.
	// This is a mock implementation, so we'll keep it simple.
	return peer
}

// AddPeer implements Reactor.
func (mr *MockReactor) AddPeer(peer p2p.Peer) {
	mr.peer = peer
}

// RemovePeer implements Reactor.
func (mr *MockReactor) RemovePeer(peer p2p.Peer, reason interface{}) {
	// Handle the removal of a peer.
	// In this mock implementation, we'll simply log the event.
	mr.Logger.Info("MockReactor removed a peer", "peer", peer.ID(), "reason", reason)
}

// Receive implements Reactor.
func (mr *MockReactor) Receive(chID byte, peer p2p.Peer, msgBytes []byte) {
	msg := &protomem.Message{}
	err := proto.Unmarshal(msgBytes, msg)
	if err != nil {
		fmt.Println("failure to unmarshal")
		// panic(err)
	}
	uw, err := msg.Unwrap()
	if err != nil {
		fmt.Println("failure to unwrap")
		// panic(err)
	}
	mr.ReceiveEnvelope(p2p.Envelope{
		ChannelID: chID,
		Src:       peer,
		Message:   uw,
	})
}

// ReceiveEnvelope implements Reactor.
// It processes one of three messages: Txs, SeenTx, WantTx.
func (mr *MockReactor) ReceiveEnvelope(e p2p.Envelope) {
	size := 0
	switch msg := e.Message.(type) {
	case *protomem.Txs:
		for _, tx := range msg.Txs {
			size += len(tx)
		}
	default:
		panic("Unexpected message type")
	}
	t := time.Now()
	mr.mtx.Lock()
	mr.Traces = append(mr.Traces, Trace{
		ReceiveTime: t,
		Size:        size,
		Channel:     e.ChannelID,
	})
	mr.mtx.Unlock()
}

func (mr *MockReactor) SendBytes(chID byte, count int) bool {
	b := make([]byte, count)
	_, err := rand.Read(b)
	if err != nil {
		mr.Logger.Error("Failed to generate random bytes")
		return false
	}
	txs := &protomem.Txs{Txs: [][]byte{b}}
	return p2p.SendEnvelopeShim(mr.peer, p2p.Envelope{
		Message:   txs,
		ChannelID: chID,
		Src:       mr.peer,
	}, mr.Logger)
}

func (mr *MockReactor) FillChannel(chID byte, count, msgSize int) (bool, int, time.Duration) {
	start := time.Now()
	for i := 0; i < count; i++ {
		success := mr.SendBytes(chID, msgSize)
		if !success {
			end := time.Now()
			return success, i, end.Sub(start)
		}
	}
	end := time.Now()
	return true, count, end.Sub(start)
}

func (mr *MockReactor) FloodChannel(wg *sync.WaitGroup, chID byte, d time.Duration) {
	wg.Add(1)
	go func(d time.Duration) {
		start := time.Now()
		defer wg.Done()
		for time.Since(start) < d {
			mr.SendBytes(chID, tmrand.Intn(100000))
		}

	}(d)
}
