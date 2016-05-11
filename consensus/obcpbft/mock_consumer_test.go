/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package obcpbft

import (
	"github.com/hyperledger/fabric/consensus"
	pb "github.com/hyperledger/fabric/protos"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/viper"
)

type consumerEndpoint struct {
	*testEndpoint
	consumer     pbftConsumer
	execTxResult func([]*pb.Transaction) ([]byte, error)
}

func (ce *consumerEndpoint) stop() {
	ce.consumer.Close()
}

func (ce *consumerEndpoint) isBusy() bool {
	if ce.consumer.getPBFTCore().timerActive || ce.consumer.getPBFTCore().currentExec != nil {
		ce.net.debugMsg("Reporting busy because of timer or currentExec\n")
		return true
	}

	select {
	case <-ce.consumer.idleChannel():
	default:
		ce.net.debugMsg("Reporting busy because consumer not idle\n")
		return true
	}

	select {
	case <-ce.consumer.getPBFTCore().idleChan:
		ce.net.debugMsg("Reporting busy because pbft not idle\n")
	default:
		return true
	}
	//}

	return false
}

func (ce *consumerEndpoint) deliver(msg []byte, senderHandle *pb.PeerID) {
	ce.consumer.RecvMsg(&pb.Message{Type: pb.Message_CONSENSUS, Payload: msg}, senderHandle)
}

type completeStack struct {
	*consumerEndpoint
	*noopSecurity
	*MockLedger
	mockPersist
}

func (cs *completeStack) SkipTo(tag uint64, id []byte, peers []*pb.PeerID) {
	go func() {
		meta := &Metadata{tag}
		metaRaw, _ := proto.Marshal(meta)
		cs.simulateStateTransfer(metaRaw, id, peers)
		cs.consumer.StateUpdate(tag, id)
	}()
}

type pbftConsumer interface {
	innerStack
	consensus.Consenter
	getPBFTCore() *pbftCore
	Close()
	idleChannel() <-chan struct{}
}

type consumerNetwork struct {
	*testnet
	mockLedgers []*MockLedger
}

func (cnet *consumerNetwork) GetLedgerByPeerID(peerID *pb.PeerID) (consensus.ReadOnlyLedger, bool) {
	id, err := getValidatorID(peerID)
	if nil != err {
		return nil, false
	}
	return cnet.mockLedgers[id], true
}

func makeConsumerNetwork(N int, makeConsumer func(id uint64, config *viper.Viper, stack consensus.Stack) pbftConsumer, initFNs ...func(*consumerEndpoint)) *consumerNetwork {
	twl := consumerNetwork{mockLedgers: make([]*MockLedger, N)}

	endpointFunc := func(id uint64, net *testnet) endpoint {
		tep := makeTestEndpoint(id, net)
		ce := &consumerEndpoint{
			testEndpoint: tep,
		}

		ml := NewMockLedger(&twl)
		twl.mockLedgers[id] = ml

		cs := &completeStack{
			consumerEndpoint: ce,
			noopSecurity:     &noopSecurity{},
			MockLedger:       ml,
		}

		ce.consumer = makeConsumer(id, loadConfig(), cs)
		ce.consumer.getPBFTCore().N = N
		ce.consumer.getPBFTCore().f = (N - 1) / 3

		for _, fn := range initFNs {
			fn(ce)
		}

		return ce
	}

	twl.testnet = makeTestnet(N, endpointFunc)
	return &twl
}
