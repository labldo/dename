// Copyright 2014 The Dename Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may not
// use this file except in compliance with the License. You may obtain a copy of
// the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations under
// the License.

package server

import (
	"gopkg.in/gcfg.v1"
	"encoding/base64"
	"fmt"
	"github.com/agl/ed25519"
	. "github.com/andres-erbsen/dename/protocol"
	"github.com/gogo/protobuf/proto"
	"io/ioutil"
	"log"
	"net"
	"os"
)

type ServerConfig struct {
	Backend struct {
		DataDirectory      string
		SigningKeyPath     string
		Listen             string // address:port
		ConsensusThreshold int
	}
	Frontend struct {
		Listen           string // address:port
		TransportKeyPath string
		InviteKeyPath    string
	}
	Server map[string]*struct { // back-end address
		PublicKey string // base64 encoded
		IsCore    bool
	}
}

func serverFromConfig(cfg *ServerConfig) (*backNet, *server, error) {
	skBytes, err := ioutil.ReadFile(cfg.Backend.SigningKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read signing key: %v", err)
	}
	if len(skBytes) != 64 {
		return nil, nil, fmt.Errorf("Signing key must be 64 bytes (got %d)", len(skBytes))
	}
	sk := new([ed25519.PrivateKeySize]byte)
	copy(sk[:], skBytes[:64])

	var inviteKey []byte
	if cfg.Frontend.InviteKeyPath != "" {
		inviteKey, err = ioutil.ReadFile(cfg.Frontend.InviteKeyPath)
		if err != nil {
			log.Fatalf("read invite key: %v:", err)
		}
	}
	fe := NewFrontend()
	fe.inviteMacKey = inviteKey

	comm := &communicator{servers: make(map[uint64]*ServerInfo)}
	bn := &backNet{servers: comm.servers, handler: comm.OnMessage, subscribers: make(map[net.Conn]struct{})}
	comm.serverNet = bn
	comm.subscribers = bn

	if len(cfg.Server) == 0 {
		return nil, nil, fmt.Errorf("no servers in the config file")
	}

	for address, s := range cfg.Server {
		pkData, err := base64.StdEncoding.DecodeString(s.PublicKey)
		if err != nil {
			return nil, nil, err
		}
		var pk Profile_PublicKey
		if err = proto.Unmarshal(pkData, &pk); err != nil {
			return nil, nil, err
		}
		bn.servers[pk.ID()] = &ServerInfo{
			Profile_PublicKey: pk,
			ID:                pk.ID(),
			Addr:              address,
			IsCore:            s.IsCore,
			messageBroker:     &MessageBroker{serverID: pk.ID(), servernet: comm.serverNet},
		}
	}
	server, err := OpenServer(cfg.Backend.DataDirectory, sk, comm, fe, cfg.Backend.ConsensusThreshold)
	if err != nil {
		return nil, nil, fmt.Errorf("openserver: %s", err)
	}
	fe.isCore = server.communicator.servers[server.id].IsCore
	subscribe := !fe.isCore
	for _, s := range bn.servers {
		s.messageBroker.stop = server.stop
		s.messageBroker.subscribe = subscribe
	}
	bn.stop = server.stop
	bn.waitStop = &server.waitStop
	fe.stop = server.stop
	fe.waitStop = &server.waitStop
	return bn, server, err
}

func StartFromConfigFile(path string) *server {
	cfg := new(ServerConfig)
	if err := gcfg.ReadFileInto(cfg, path); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	return StartFromConfig(cfg)
}

func StartFromConfig(cfg *ServerConfig) *server {
	backnet, server, err := serverFromConfig(cfg)
	if err != nil {
		log.Fatalf("Startup failed: %v", err)
	}
	server.waitStop.Add(1)
	go server.Run()
	if cfg.Backend.Listen != "" {
		ln, err := net.Listen("tcp", cfg.Backend.Listen)
		if err != nil {
			log.Fatal(err)
		}
		server.waitStop.Add(1)
		go backnet.listenBackend(ln)
	}
	if cfg.Frontend.Listen != "" {
		var sk [32]byte
		skFile, err := os.Stat(cfg.Frontend.TransportKeyPath)
		if err != nil {
			log.Fatalf("cannot stat secret key file (%s): %s", cfg.Frontend.TransportKeyPath, err)
		}
		if !skFile.Mode().IsRegular() {
			log.Fatalf("%s is not a regular file", skFile.Name())
		}
		if skFile.Mode().Perm()&077 != 0 {
			log.Fatalf("%s is unprotected", skFile.Name())
		}
		skData, err := ioutil.ReadFile(cfg.Frontend.TransportKeyPath)
		if err != nil {
			log.Fatalf("read transport secret key: %s", err)
		}
		if len(skData) != 32 {
			log.Fatalf("malformed secret key (expected %d bytes, got %d)", 32, len(skData))
		}
		copy(sk[:], skData)

		ln, err := net.Listen("tcp", cfg.Frontend.Listen)
		if err != nil {
			log.Fatalf("server: listen: %s", err)
		}
		server.waitStop.Add(1)
		go server.frontend.listenForClients(ln, &sk)
	}
	return server
}
