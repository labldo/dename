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

package client

import (
	"bytes"
	"golang.org/x/net/proxy"
	"fmt"
	"github.com/agl/ed25519"
	"github.com/andres-erbsen/chatterbox/transport"
	. "github.com/andres-erbsen/dename/protocol"
	"github.com/gogo/protobuf/proto"
	"io"
	"net"
	"time"
)

var true_ = true
var pad_to uint64 = 4 << 10

type verifier struct {
	name string
	pk   *Profile_PublicKey
}

type server struct {
	address     string
	timeout     time.Duration
	transportPK [32]byte
}

type Client struct {
	now                         func() time.Time
	dialer                      proxy.Dialer
	freshnessThreshold          time.Duration
	freshnessSignaturesRequired int
	consensusSignaturesRequired int
	verifier                    map[uint64]*verifier
	update                      []*server
	lookup                      []*server
}

func (c *Client) connect(s *server) (*transport.Conn, error) {
	var plainconn net.Conn
	plainconn, err := c.dialer.Dial("tcp", s.address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %s", s.address, err)
	}
	plainconn.SetDeadline(time.Now().Add(s.timeout))
	conn, _, err := transport.Handshake(plainconn, nil, nil, &s.transportPK, 1<<12)
	if err != nil {
		plainconn.Close()
		return nil, fmt.Errorf("transport handshake %s: %s", s.address, err)
	}
	return conn, nil
}

func (c *Client) atSomeServer(servers []*server, f func(*transport.Conn) (bool, error)) (err error) {
	for _, server := range servers {
		var conn *transport.Conn
		conn, err = c.connect(server)
		if err != nil {
			continue
		}
		var done bool
		done, err = f(conn)
		conn.Close()
		if done {
			return err
		}
	}
	return err
}

// Lookup retrieves the profile that corresponds to name from any server in the
// client's config. It is guaranteed that at least SignaturesRequired of the
// servers have confirmed the correctness of the (name, profile) mapping and
// that Freshness.SignaturesRequired have done this within Freshness.Threshold.
func (c *Client) Lookup(name string) (profile *Profile, err error) {
	profile, _, err = c.LookupReply(name)
	return
}

func (c *Client) LookupReply(name string) (profile *Profile, reply *ClientReply, err error) {
	err = c.atSomeServer(c.lookup, func(conn *transport.Conn) (bool, error) {
		rq := &ClientMessage{PeekState: &true_, ResolveName: []byte(name), PadReplyTo: &pad_to}
		if _, err = conn.WriteFrame(Pad(PBEncode(rq), 256)); err != nil {
			return false, err
		}
		if reply, err = readReply(conn); err != nil {
			return false, err
		}
		if profile, err = c.LookupFromReply(name, reply); err != nil {
			return true, err
		}
		return true, nil
	})
	return
}

func (c *Client) LookupFromReply(name string, reply *ClientReply) (profile *Profile, err error) {
	var root []byte
	root, err = c.VerifyConsensus(reply.StateConfirmations)
	if err != nil {
		return
	}
	profileBs, err := VerifyResolveAgainstRoot(root, name, reply.LookupNodes)
	if err != nil {
		return
	}
	if profileBs == nil {
		profile = nil
		return
	}
	profile = new(Profile)
	err = proto.Unmarshal(profileBs, profile)
	if err != nil {
		return nil, err
	}
	expirationTime := time.Unix(int64(*profile.ExpirationTime), 0)
	if expirationTime.Before(time.Now().Add(MAX_VALIDITY_PERIOD * time.Second / 2)) {
		return profile, errOutOfDate{fmt.Sprintf("the profile is out of date and will be erased completely on %s", expirationTime)}
	}
	return profile, nil
}

var (
	ErrRegistrationDisabled = fmt.Errorf("registration disabled")
	ErrInviteInvalid        = fmt.Errorf("invite not valid")
	ErrInviteUsed           = fmt.Errorf("invite already used")
	ErrNotAuthorized        = fmt.Errorf("not authorized")
	ErrCouldntVerify        = fmt.Errorf("could not verify the correctness of the response")
)

// Enact is a low-level function that completes a fully assembled profile
type errOutOfDate struct {
	string
}

func (e errOutOfDate) Error() string {
	return e.string
}

func IsErrOutOfDate(err error) bool {
	switch err.(type) {
	case errOutOfDate:
		return true
	default:
		return false
	}
}

// Enact is a low-level function that completes an already complete profile
// operation at any known server. You probably want to use Register, Modify or
// Transfer instead.
func (c *Client) Enact(op *SignedProfileOperation, invite []byte) (err error) {
	err = c.atSomeServer(c.update, func(conn *transport.Conn) (bool, error) {
		msg := &ClientMessage{ModifyProfile: op, InviteCode: invite}
		_, err = conn.WriteFrame(Pad(PBEncode(msg), int(pad_to)))
		if err != nil {
			return false, err
		}
		var reply *ClientReply
		reply, err = readReply(conn)
		if err != nil {
			return false, err
		}
		switch reply.GetStatus() {
		case ClientReply_OK:
			return true, nil
		case ClientReply_REGISTRATION_DISABLED:
			return false, ErrRegistrationDisabled
		case ClientReply_INVITE_INVALID:
			return false, ErrInviteInvalid
		case ClientReply_INVITE_USED:
			return false, ErrInviteUsed
		case ClientReply_NOT_AUTHORIZED:
			return false, ErrNotAuthorized
		default:
			return false, fmt.Errorf("unknown status code")
		}
	})
	return
}

type byteReader struct{ io.Reader }

func (r byteReader) ReadByte() (byte, error) {
	var ret [1]byte
	_, err := io.ReadFull(r, ret[:])
	return ret[0], err
}

func readReply(conn *transport.Conn) (reply *ClientReply, err error) {
	buf := make([]byte, pad_to)
	sz, err := conn.ReadFrame(buf)
	if err != nil {
		return
	}
	reply = new(ClientReply)
	err = proto.Unmarshal(Unpad(buf[:sz]), reply)
	return
}

// Low-level convenience function to create a SignedProfileOperation with no
// signatures. You probably want to use Register, Modify or Transfer instead.
func MakeOperation(name string, profile *Profile) *SignedProfileOperation {
	return &SignedProfileOperation{
		ProfileOperation: PBEncode(&SignedProfileOperation_ProfileOperationT{
			Name:       []byte(name),
			NewProfile: PBEncode(profile),
		}),
	}
}

// Creates a signed operation structure to transfer name to profile. To make
// this change take effect, the recipient has to call AcceptTransfer with the
// secret key whose public counterpart is in profile.
func TransferProposal(sk *[ed25519.PrivateKeySize]byte, name string,
	profile *Profile) *SignedProfileOperation {
	return OldSign(sk, MakeOperation(name, profile))
}

// Gives the old owner's signature for op using sk
func OldSign(sk *[ed25519.PrivateKeySize]byte, op *SignedProfileOperation) *SignedProfileOperation {
	msg := append([]byte("ModifyProfileOld\x00"), op.ProfileOperation...)
	op.OldProfileSignature = ed25519.Sign(sk, msg)[:]
	return op
}

// Gives the new owner's signature for op using sk
func NewSign(sk *[ed25519.PrivateKeySize]byte, op *SignedProfileOperation) *SignedProfileOperation {
	msg := append([]byte("ModifyProfileNew\x00"), op.ProfileOperation...)
	op.NewProfileSignature = ed25519.Sign(sk, msg)[:]
	return op
}

// Register associates a profile with a name. The invite is used to convince
// the server that we are indeed allowed a new name, it is not associated with
// the profile in any way. If profile.Version is set, it must be 0.
func (c *Client) Register(sk *[ed25519.PrivateKeySize]byte, name string, profile *Profile, invite []byte) error {
	return c.Enact(NewSign(sk, MakeOperation(name, profile)), invite)
}

// AcceptTransfer uses the new secret key and the transfer operation generated
// using the old secret key to associate the op.Name with op.NewProfile.
func (c *Client) AcceptTransfer(sk *[ed25519.PrivateKeySize]byte, op *SignedProfileOperation) error {
	return c.Enact(NewSign(sk, op), nil)
}

// Modify uses a secret key to associate name with profile. The caller must
// ensure that profile.Version is strictly greater than the version of the
// currently registered profile; it is usually good practice to increase the
// version by exactly one. In many cases it is also desireable to bump to
// expiration time to just slightly less than one year into the future.
func (c *Client) Modify(sk *[ed25519.PrivateKeySize]byte, name string, profile *Profile) error {
	return c.Enact(NewSign(sk, OldSign(sk, MakeOperation(name, profile))), nil)
}

// VerifiyConsensus performs the low-level checks to see whether a set of
// statements made by the servers is sufficient to consider the state contained
// by them to be canonical.
func (c *Client) VerifyConsensus(signedHashOfStateMsgs []*SignedServerMessage) (
	rootHash []byte, err error) {
	consensusServers := make(map[uint64]struct{})
	freshnessServers := make(map[uint64]struct{})
	for _, signedMsg := range signedHashOfStateMsgs {
		msg := new(SignedServerMessage_ServerMessage)
		if err = proto.Unmarshal(signedMsg.Message, msg); err != nil {
			continue
		}
		v, ok := c.verifier[*msg.Server]
		if !ok || v.pk.Ed25519 == nil {
			continue
		}
		var pk_ed [ed25519.PublicKeySize]byte
		copy(pk_ed[:], v.pk.Ed25519)
		var sig_ed [ed25519.SignatureSize]byte
		copy(sig_ed[:], signedMsg.Signature)
		if !ed25519.Verify(&pk_ed, append([]byte("msg\x00"), signedMsg.Message...), &sig_ed) {
			continue
		}
		if rootHash == nil {
			rootHash = msg.HashOfState
		} else if !bytes.Equal(rootHash, msg.HashOfState) {
			return nil, fmt.Errorf("verifyConsensus: state hashes differ")
		}
		consensusServers[*msg.Server] = struct{}{}
		if !time.Unix(int64(*msg.Time), 0).Add(c.freshnessThreshold).After(c.now()) {
			continue
		}
		freshnessServers[*msg.Server] = struct{}{}
	}
	if len(consensusServers) < c.consensusSignaturesRequired {
		return nil, fmt.Errorf("not enough valid signatures for consensus (%d out of %d): %s", len(consensusServers), c.consensusSignaturesRequired, printVerifiers(consensusServers, c.verifier))
	}
	if len(freshnessServers) < c.freshnessSignaturesRequired {
		return nil, fmt.Errorf("not enough fresh signatures (%d out of %d): %s", len(freshnessServers), c.freshnessSignaturesRequired, printVerifiers(freshnessServers, c.verifier))
	}
	return rootHash, nil
}

func printVerifiers(vs map[uint64]struct{}, verifier map[uint64]*verifier) string {
	ret := ""
	for id := range vs {
		if ret != "" {
			ret += ", "
		}
		ret += verifier[id].name
	}
	return ret
}
