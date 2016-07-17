package smb2

import (
	"crypto/rand"
	"fmt"
	"sync"

	. "github.com/hirochachacha/smb2/internal/erref"
	. "github.com/hirochachacha/smb2/internal/smb2"
)

// Negotiator contains options for func (*Dialer) Dial.
type Negotiator struct {
	RequireMessageSigning bool     // enforce signing?
	ClientGuid            [16]byte // if it's zero, generated by crypto/rand.
	SpecifiedDialect      uint16   // if it's zero, clientDialects is used. (See feature.go for more details)
}

func (n *Negotiator) makeRequest() (*NegotiateRequest, error) {
	req := new(NegotiateRequest)

	if n.RequireMessageSigning {
		req.SecurityMode = SMB2_NEGOTIATE_SIGNING_REQUIRED
	} else {
		req.SecurityMode = SMB2_NEGOTIATE_SIGNING_ENABLED
	}

	req.Capabilities = clientCapabilities

	if n.ClientGuid == zero {
		_, err := rand.Read(req.ClientGuid[:])
		if err != nil {
			return nil, &InternalError{err.Error()}
		}
	} else {
		req.ClientGuid = n.ClientGuid
	}

	if n.SpecifiedDialect != UnknownSMB {
		req.Dialects = []uint16{n.SpecifiedDialect}

		switch n.SpecifiedDialect {
		case SMB202:
		case SMB210:
		case SMB300:
		case SMB302:
		case SMB311:
			req.HashAlgorithms = clientHashAlgorithms
			req.HashSalt = make([]byte, 32)
			if _, err := rand.Read(req.HashSalt); err != nil {
				return nil, &InternalError{err.Error()}
			}
			req.Ciphers = clientCiphers
		default:
			return nil, &InternalError{"unsupported dialect specified"}
		}
	} else {
		req.Dialects = clientDialects

		req.HashAlgorithms = clientHashAlgorithms
		req.HashSalt = make([]byte, 32)
		if _, err := rand.Read(req.HashSalt); err != nil {
			return nil, &InternalError{err.Error()}
		}
		req.Ciphers = clientCiphers
	}

	return req, nil
}

func (n *Negotiator) negotiate(t transport, a *account) (*conn, error) {
	conn := &conn{
		t:            t,
		sessionTable: make(map[uint64]*session),
		outstanding: struct {
			sync.Mutex
			Requests map[uint64]*requestResponse
		}{
			Requests: make(map[uint64]*requestResponse),
		},
		account: a,
	}

	go conn.runReciever()

retry:
	req, err := n.makeRequest()
	if err != nil {
		return nil, err
	}

	req.CreditCharge = 1

	res, err := conn.sendRecv(SMB2_NEGOTIATE, req)
	if err != nil {
		return nil, err
	}

	r := NegotiateResponseDecoder(res)
	if r.IsInvalid() {
		return nil, &InvalidResponseError{"broken negotiate response format"}
	}

	if r.DialectRevision() == SMB2 {
		n.SpecifiedDialect = SMB210

		goto retry
	}

	if n.SpecifiedDialect != UnknownSMB && n.SpecifiedDialect != r.DialectRevision() {
		return nil, &InvalidResponseError{"unexpected dialect returned"}
	}

	conn.requireSigning = n.RequireMessageSigning || r.SecurityMode()&SMB2_NEGOTIATE_SIGNING_REQUIRED != 0
	conn.capabilities = clientCapabilities & r.Capabilities()
	conn.dialect = r.DialectRevision()
	conn.maxTransactSize = r.MaxTransactSize()
	conn.maxReadSize = r.MaxReadSize()
	conn.maxWriteSize = r.MaxWriteSize()
	// conn.gssNegotiateToken = r.SecurityBuffer()
	conn.clientGuid = n.ClientGuid
	copy(conn.serverGuid[:], r.ServerGuid())
	conn.sequenceWindow = 1

	list := r.NegotiateContextList()
	for count := r.NegotiateContextCount(); count > 0; count-- {
		ctx := NegotiateContextDecoder(list)
		if ctx.IsInvalid() {
			return nil, &InvalidResponseError{"broken negotiate context format"}
		}

		switch ctx.ContextType() {
		case SMB2_PREAUTH_INTEGRITY_CAPABILITIES:
			d := HashContextDataDecoder(ctx.Data())
			if d.IsInvalid() {
				return nil, &InvalidResponseError{"broken hash context data format"}
			}

			algs := d.HashAlgorithms()
			salt := d.Salt()

			if len(algs) != 1 {
				return nil, &InvalidResponseError{"multiple hash algorithms"}
			}

			conn.preauthIntegrityHashId = algs[0]
			conn.preauthIntegrityHashValue = salt

			switch conn.preauthIntegrityHashId {
			case SHA512:
			default:
				return nil, &InvalidResponseError{"unknown hash algorithm"}
			}
		case SMB2_ENCRYPTION_CAPABILITIES:
			d := CipherContextDataDecoder(ctx.Data())
			if d.IsInvalid() {
				return nil, &InvalidResponseError{"broken cipher context data format"}
			}

			ciphs := d.Ciphers()

			if len(ciphs) != 1 {
				return nil, &InvalidResponseError{"multiple cipher algorithms"}
			}

			conn.cipherId = ciphs[0]

			switch conn.cipherId {
			case AES128CCM:
			case AES128GCM:
			default:
				return nil, &InvalidResponseError{"unknown cipher algorithm"}
			}
		default:
			// skip unsupported context
		}

		off := ctx.Next()

		if len(list) < off {
			list = nil
		} else {
			list = list[off:]
		}
	}

	return conn, nil
}

type requestResponse struct {
	creditRequest uint16
	recv          chan []byte
	err           error
}

type conn struct {
	t transport

	sessionTable map[uint64]*session
	outstanding  struct {
		sync.Mutex
		Requests map[uint64]*requestResponse
	}
	sequenceWindow uint64
	// gssNegotiateToken         []byte
	maxTransactSize           uint32
	maxReadSize               uint32
	maxWriteSize              uint32
	serverGuid                [16]byte
	requireSigning            bool
	dialect                   uint16
	clientGuid                [16]byte
	capabilities              uint32
	preauthIntegrityHashId    uint16
	preauthIntegrityHashValue []byte
	cipherId                  uint16

	account *account

	m sync.Mutex
}

func (conn *conn) sendRecv(cmd uint16, req Packet) (res []byte, err error) {
	rr, err := conn.send(req)
	if err != nil {
		return nil, err
	}

	pkt, err := conn.recv(rr)
	if err != nil {
		return nil, err
	}

	return accept(cmd, pkt)
}

func (conn *conn) requestCreditCharge(payloadSize int) (creditCharge uint16, grantedPayloadSize int) {
	if conn.capabilities&SMB2_GLOBAL_CAP_LARGE_MTU == 0 {
		creditCharge = 1
	} else {
		creditCharge = uint16((payloadSize-1)/(64*1024) + 1)
	}

	creditCharge, complete := conn.account.request(creditCharge)
	if complete {
		return creditCharge, payloadSize
	}

	return creditCharge, 64 * 1024 * int(creditCharge)
}

func (conn *conn) send(req Packet) (rr *requestResponse, err error) {
	return conn.sendWith(req, nil, nil)
}

func (conn *conn) sendWith(req Packet, s *session, tc *treeConn) (rr *requestResponse, err error) {
	conn.m.Lock()
	defer conn.m.Unlock()

	hdr := req.Header()

	var msgId uint64

	if _, ok := req.(*CancelRequest); !ok {
		msgId = conn.sequenceWindow

		creditCharge := hdr.CreditCharge

		conn.sequenceWindow += uint64(creditCharge)
		if hdr.CreditRequest == 0 {
			hdr.CreditRequest = creditCharge
		}

		hdr.CreditRequest += conn.account.opening()
	}

	hdr.MessageId = msgId
	if s != nil {
		hdr.SessionId = s.sessionId

		if tc != nil {
			hdr.TreeId = tc.treeId
		}
	}

	pkt := make([]byte, req.Size())

	req.Encode(pkt)

	if s != nil {
		// windows 10 doesn't support signing on session setup request.
		if _, ok := req.(*SessionSetupRequest); !ok {
			pkt = s.sign(pkt)

			if s.sessionFlags&SMB2_SESSION_FLAG_ENCRYPT_DATA != 0 || (tc != nil && tc.shareFlags&SMB2_SHAREFLAG_ENCRYPT_DATA != 0) {
				pkt, err = s.encrypt(pkt)
				if err != nil {
					return nil, &InternalError{err.Error()}
				}
			}
		}
	}

	rr = &requestResponse{
		creditRequest: hdr.CreditRequest,
		recv:          make(chan []byte, 1),
	}

	conn.outstanding.Lock()

	conn.outstanding.Requests[msgId] = rr

	conn.outstanding.Unlock()

	_, err = conn.t.Write(pkt)
	if err != nil {
		conn.outstanding.Lock()

		delete(conn.outstanding.Requests, msgId)

		conn.outstanding.Unlock()

		return nil, &TransportError{err}
	}

	return rr, nil
}

func (conn *conn) recv(rr *requestResponse) ([]byte, error) {
	pkt := <-rr.recv
	if rr.err != nil {
		return nil, rr.err
	}
	return pkt, nil
}

func accept(cmd uint16, pkt []byte) (res []byte, err error) {
	p := PacketCodec(pkt)
	if command := p.Command(); cmd != command {
		return nil, &InvalidResponseError{fmt.Sprintf("expected command: %v, got %v", cmd, command)}
	}

	if status := NtStatus(p.Status()); status != STATUS_SUCCESS {
		return nil, acceptError(p)
	}

	return p.Data(), nil
}

func acceptError(p PacketCodec) error {
	r := ErrorResponseDecoder(p.Data())
	if r.IsInvalid() {
		return &InvalidResponseError{"broken error response format"}
	}

	eData := r.ErrorData()

	if count := r.ErrorContextCount(); count != 0 {
		data := make([][]byte, count)
		for i := range data {
			ctx := ErrorContextResponseDecoder(eData)
			if ctx.IsInvalid() {
				return &InvalidResponseError{"broken error context response format"}
			}

			data[i] = ctx.ErrorContextData()

			eData = ctx.Next()
		}
		return &ResponseError{Code: p.Status(), data: data}
	}
	return &ResponseError{Code: p.Status(), data: [][]byte{eData}}
}

func (conn *conn) runReciever() {
	var err error

	for {
		n, e := conn.t.ReadSize()
		if e != nil {
			err = &TransportError{e}

			break
		}

		pkt := make([]byte, n)

		_, e = conn.t.Read(pkt)
		if e != nil {
			err = &TransportError{e}

			break
		}

		p := PacketCodec(pkt)
		if p.IsInvalid() {
			t := TransformCodec(pkt)
			if t.IsInvalid() {
				err = &InvalidResponseError{"broken packet header format"}

				break
			}

			if t.Flags() != Encrypted {
				err = &InvalidResponseError{"encrypted flag is not on"}

				break
			}

			s, ok := conn.sessionTable[t.SessionId()]
			if !ok {
				err = &InvalidResponseError{"unknown session id returned"}

				break
			}

			pkt, err = s.decrypt(pkt)
			if err != nil {
				err = &InvalidResponseError{err.Error()}

				break
			}

			p = PacketCodec(pkt)
		} else {
			if p.MessageId() != 0xFFFFFFFFFFFFFFFF {
				if p.Flags()&SMB2_FLAGS_SIGNED != 0 {
					s, ok := conn.sessionTable[p.SessionId()]
					if !ok {
						err = &InvalidResponseError{"unknown session id returned"}

						break
					}

					if !s.verify(pkt) {
						err = &InvalidResponseError{"unverified packet returned"}

						break
					}
				} else {
					if conn.requireSigning {
						if _, ok := conn.sessionTable[p.SessionId()]; ok {
							err = &InvalidResponseError{"signing required"}

							break
						}
					}
				}
			}
		}

		msgId := p.MessageId()

		conn.outstanding.Lock()

		rr, ok := conn.outstanding.Requests[msgId]
		if !ok {
			err = &InvalidResponseError{"unknown message id returned"}

			conn.outstanding.Unlock()

			break
		}
		delete(conn.outstanding.Requests, msgId)

		conn.outstanding.Unlock()

		for {
			conn.account.grant(p.CreditResponse(), rr.creditRequest)

			next := p.NextCommand()
			if next == 0 {
				rr.recv <- pkt

				break
			}

			rr.recv <- pkt[:next]

			pkt = pkt[next:]

			p = PacketCodec(pkt)
		}
	}

	for _, rr := range conn.outstanding.Requests {
		rr.err = err

		select {
		case rr.recv <- nil:
		default:
		}
	}
}
