// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/dcrd/wire"

	errs "github.com/decred/dcrpool/errors"
)

// Message types.
const (
	UnknownMessage = iota
	RequestMessage
	ResponseMessage
	NotificationMessage
)

// Handler types.
const (
	Authorize           = "mining.authorize"
	Subscribe           = "mining.subscribe"
	ExtraNonceSubscribe = "mining.extranonce.subscribe"
	SetDifficulty       = "mining.set_difficulty"
	Notify              = "mining.notify"
	Submit              = "mining.submit"
)

// Error codes.
const (
	Unknown            = 20
	StaleJob           = 21
	DuplicateShare     = 22
	LowDifficultyShare = 23
	UnauthorizedWorker = 24
	NotSubscribed      = 25
)

// Stratum constants.
const (
	ExtraNonce2Size = 4
)

// StratumError represents a stratum error message.
type StratumError struct {
	Code      uint32  `json:"code"`
	Message   string  `json:"message"`
	Traceback *string `json:"traceback"`
}

// NewStratumError creates a stratum error instance.
func NewStratumError(code uint32, err error) *StratumError {
	var msg string

	switch code {
	case StaleJob:
		msg = "Stale Job"
	case DuplicateShare:
		msg = "Duplicate share"
	case LowDifficultyShare:
		msg = "Low difficulty share"
	case UnauthorizedWorker:
		msg = "Unauthorized worker"
	case NotSubscribed:
		msg = "Not subscribed"
	case Unknown:
		fallthrough
	default:
		msg = "Other/Unknown"
	}

	if err != nil {
		msg = fmt.Sprintf("%s: %v", msg, err)
	}

	return &StratumError{
		Code:    code,
		Message: msg,
	}
}

// MarshalJSON marshals the stratum error into valid JSON.
func (s *StratumError) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{s.Code, s.Message, s.Traceback})
}

// UnmarshalJSON unmarshals the provided JSON bytes into a stratum error.
func (s *StratumError) UnmarshalJSON(p []byte) error {
	var tmp []interface{}
	if err := json.Unmarshal(p, &tmp); err != nil {
		return err
	}
	s.Code = uint32(tmp[0].(float64))
	s.Message = tmp[1].(string)
	if tmp[2] != nil {
		trace := tmp[2].(string)
		s.Traceback = &trace
	}

	return nil
}

// String returns a stringified representation of the stratum error.
func (s *StratumError) String() string {
	return s.Message
}

// Message defines a message interface.
type Message interface {
	MessageType() int
	String() string
}

// Request defines a request message.
type Request struct {
	ID     *uint64     `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// MessageType returns the request message type.
func (req *Request) MessageType() int {
	return RequestMessage
}

// String returns the string representation of the request message type.
func (req *Request) String() string {
	b, err := json.Marshal(req)
	if err != nil {
		log.Errorf("unable to marshal request: %v", err)
		return ""
	}

	return string(b)
}

// NewRequest creates a request instance.
func NewRequest(id *uint64, method string, params interface{}) *Request {
	return &Request{
		ID:     id,
		Method: method,
		Params: params,
	}
}

// Response defines a response message.
type Response struct {
	ID     uint64        `json:"id"`
	Error  *StratumError `json:"error"`
	Result interface{}   `json:"result,omitempty"`
}

// String returns the string representation of the response message type.
func (req *Response) String() string {
	b, err := json.Marshal(req)
	if err != nil {
		log.Errorf("unable to marshal response: %v", err)
		return ""
	}

	return string(b)
}

// MessageType returns the response message type.
func (req *Response) MessageType() int {
	return ResponseMessage
}

// NewResponse creates a response instance.
func NewResponse(id uint64, result interface{}, err *StratumError) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: result,
	}
}

// IdentifyMessage determines the received message type. It returns the message
// cast to the appropriate message type, the message type and an error type.
func IdentifyMessage(data []byte) (Message, int, error) {
	var req Request
	err := json.Unmarshal(data, &req)
	if err != nil {
		return nil, UnknownMessage, err
	}

	if req.Method != "" {
		if req.ID == nil {
			return &req, NotificationMessage, nil
		}
		return &req, RequestMessage, nil
	}

	var resp Response
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, UnknownMessage, err
	}

	if resp.ID == 0 {
		return nil, UnknownMessage, fmt.Errorf("unable to parse message")
	}

	return &resp, ResponseMessage, nil
}

// AuthorizeRequest creates an authorize request message.
func AuthorizeRequest(id *uint64, name string, address string) *Request {
	format := "%s.%s"
	if name == "" || address == "" {
		format = "%s%s"
	}
	user := fmt.Sprintf(format, address, name)
	return &Request{
		ID:     id,
		Method: Authorize,
		Params: []string{user, ""},
	}
}

// ParseAuthorizeRequest resolves an authorize request into its components.
func ParseAuthorizeRequest(req *Request) (string, error) {
	const funcName = "ParseAuthorizeRequest"
	if req.Method != Authorize {
		desc := fmt.Sprintf("%s: request method is not authorize", funcName)
		return "", errs.MsgError(errs.Parse, desc)
	}

	auth, ok := req.Params.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse authorize parameters",
			funcName)
		return "", errs.MsgError(errs.Parse, desc)
	}

	if len(auth) < 2 {
		desc := fmt.Sprintf("%s: expected 2 params for authorize request, "+
			"got %d", funcName, len(auth))
		return "", errs.MsgError(errs.Parse, desc)
	}

	username, ok := auth[0].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse username parameter "+
			"for authorize request", funcName)
		return "", errs.MsgError(errs.Parse, desc)
	}

	return username, nil
}

// AuthorizeResponse creates an authorize response.
func AuthorizeResponse(id uint64, status bool, err *StratumError) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: status,
	}
}

// ParseAuthorizeResponse resolves an authorize response into its components.
func ParseAuthorizeResponse(resp *Response) (bool, *StratumError, error) {
	const funcName = "ParseAuthorizeResponse"
	status, ok := resp.Result.(bool)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse authorize response "+
			"result parameter", funcName)
		return false, nil, errs.MsgError(errs.Parse, desc)
	}

	return status, resp.Error, nil
}

// SubscribeRequest creates a subscribe request message.
func SubscribeRequest(id *uint64, userAgent string, notifyID string) *Request {
	params := []string{userAgent}
	if notifyID != "" {
		params = append(params, notifyID)
	}

	return &Request{
		ID:     id,
		Method: Subscribe,
		Params: params,
	}
}

// ParseSubscribeRequest resolves a subscribe request into its components.
func ParseSubscribeRequest(req *Request) (string, string, error) {
	const funcName = "ParseSubscribeRequest"
	if req.Method != Subscribe {
		desc := fmt.Sprintf("%s: request method is not subscribe", funcName)
		return "", "", errs.MsgError(errs.Parse, desc)
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse subscribe parameters",
			funcName)
		return "", "", errs.MsgError(errs.Parse, desc)
	}

	if len(params) == 0 {
		desc := fmt.Sprintf("%s: no user agent provided for subscribe "+
			"request", funcName)
		return "", "", errs.MsgError(errs.Parse, desc)
	}

	miner, ok := params[0].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse miner parameter for "+
			"subscribe request", funcName)
		return "", "", errs.MsgError(errs.Parse, desc)
	}

	id := ""
	if len(params) == 2 {
		id, ok = params[1].(string)
		if !ok {
			desc := fmt.Sprintf("%s: unable to parse id parameter for "+
				"subscribe request", funcName)
			return "", "", errs.MsgError(errs.Parse, desc)
		}
	}

	return miner, id, nil
}

// SubscribeResponse creates a mining.subscribe response.
func SubscribeResponse(id uint64, notifyID string, extraNonce1 string, extraNonce2Size int, err *StratumError) *Response {
	if err != nil {
		return &Response{
			ID:     id,
			Error:  err,
			Result: nil,
		}
	}

	return &Response{
		ID:    id,
		Error: nil,
		Result: []interface{}{[][]string{
			{"mining.set_difficulty", notifyID}, {"mining.notify", notifyID}},
			extraNonce1, extraNonce2Size},
	}
}

// ParseExtraNonceSubscribeRequest ensures the provided extranonce subscribe
// request is valid.
func ParseExtraNonceSubscribeRequest(req *Request) error {
	const funcName = "ParseExtraNonceSubscribeRequest"
	if req.Method != ExtraNonceSubscribe {
		desc := fmt.Sprintf("%s: request method is not extra nonce subscribe",
			funcName)
		return errs.MsgError(errs.Parse, desc)
	}
	return nil
}

// ExtraNonceSubscribeResponse creates a mining.extranonce.subscribe response.
func ExtraNonceSubscribeResponse(id uint64) *Response {
	return &Response{
		ID:     id,
		Error:  nil,
		Result: false, // The pool does not support changes to extranonce1.
	}
}

// ParseSubscribeResponse resolves a subscribe response into its components.
func ParseSubscribeResponse(resp *Response) (string, string, uint64, error) {
	const funcName = "ParseSubscribeResponse"
	if resp.Error != nil {
		desc := fmt.Sprintf("%s: %d, %s", funcName, resp.Error.Code,
			resp.Error.Message)
		return "", "", 0, errs.MsgError(errs.Parse, desc)
	}

	res, ok := resp.Result.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse subscribe response "+
			"result parameter", funcName)
		return "", "", 0, errs.MsgError(errs.Parse, desc)
	}

	ids, ok := res[0].([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse id details", funcName)
		return "", "", 0, errs.MsgError(errs.Parse, desc)
	}

	var nID string
	for _, entry := range ids {
		id, ok := ids[0].(string)
		if ok {
			if id == Notify {
				nID = ids[1].(string)
				break
			}
		}

		idSet, ok := entry.([]interface{})
		if ok {
			id, ok = idSet[0].(string)
			if !ok {
				desc := fmt.Sprintf("%s: unable to parse id", funcName)
				return "", "", 0, errs.MsgError(errs.Parse, desc)
			}

			if id != Notify {
				continue
			}

			nID = idSet[1].(string)
			break
		}
	}

	extraNonce1, ok := res[1].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse subscription ExtraNonce1 "+
			"parameter", funcName)
		return "", "", 0, errs.MsgError(errs.Parse, desc)
	}

	nonce2Size, ok := res[2].(float64)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse subscription "+
			"ExtraNonce2Size parameter", funcName)
		return "", "", 0, errs.MsgError(errs.Parse, desc)
	}

	extraNonce2Size := uint64(nonce2Size)

	return nID, extraNonce1, extraNonce2Size, nil
}

// SetDifficultyNotification creates a set difficulty notification message.
func SetDifficultyNotification(difficulty *big.Rat) *Request {
	diff, _ := difficulty.Float64()
	return &Request{
		Method: SetDifficulty,
		Params: []uint64{uint64(diff)},
	}
}

// ParseSetDifficultyNotification resolves a set difficulty notification into
// its components.
func ParseSetDifficultyNotification(req *Request) (uint64, error) {
	const funcName = "ParseSetDifficultyNotification"
	if req.Method != SetDifficulty {
		desc := fmt.Sprintf("%s: notification method is not set "+
			"difficulty", funcName)
		return 0, errs.MsgError(errs.Parse, desc)
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse set difficulty "+
			"parameters", funcName)
		return 0, errs.MsgError(errs.Parse, desc)
	}

	return uint64(params[0].(float64)), nil
}

// WorkNotification creates a work notification message.
func WorkNotification(jobID string, prevBlock string, genTx1 string, genTx2 string, blockVersion string, nBits string, nTime string, cleanJob bool) *Request {
	return &Request{
		Method: Notify,
		Params: []interface{}{jobID, prevBlock, genTx1, genTx2, []string{},
			blockVersion, nBits, nTime, cleanJob},
	}
}

// ParseWorkNotification resolves a work notification message into its components.
func ParseWorkNotification(req *Request) (string, string, string, string, string, string, string, bool, error) {
	const funcName = "ParseWorkNotification"
	if req.Method != Notify {
		desc := fmt.Sprintf("%s: notification method is not notify", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"parameters", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	jobID, ok := params[0].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"jobID parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	prevBlock, ok := params[1].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification prevBlock "+
			"parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	genTx1, ok := params[2].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification genTx1 "+
			"parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	genTx2, ok := params[3].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification genTx2 "+
			"parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	// Note that param[4] which is the list of merkle branches is not
	// applicable for decred, the final merkle root is already
	// included in the block.

	blockVersion, ok := params[5].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"blockVersion parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	nBits, ok := params[6].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"nBits parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	nTime, ok := params[7].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"nTime parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	cleanJob, ok := params[8].(bool)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse work notification "+
			"cleanJob parameter", funcName)
		return "", "", "", "", "", "", "", false, errs.MsgError(errs.Parse, desc)
	}

	return jobID, prevBlock, genTx1, genTx2, blockVersion,
		nBits, nTime, cleanJob, nil
}

// GenerateBlockHeader creates a block header from a mining.notify
// message and the extraNonce1 of the client.
func GenerateBlockHeader(blockVersionE string, prevBlockE string,
	genTx1E string, extraNonce1E string, genTx2E string) (*wire.BlockHeader, error) {
	const funcName = "GenerateBlockHeader"
	var buf bytes.Buffer
	_, _ = buf.WriteString(blockVersionE)
	_, _ = buf.WriteString(prevBlockE)
	_, _ = buf.WriteString(genTx1E)
	_, _ = buf.WriteString(extraNonce1E)
	_, _ = buf.WriteString(strings.Repeat("0", 56))
	_, _ = buf.WriteString(genTx2E)
	headerE := buf.String()

	headerD, err := hex.DecodeString(headerE)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode block header %s: %v",
			funcName, headerE, err)
		return nil, errs.MsgError(errs.Decode, desc)
	}

	var header wire.BlockHeader
	err = header.FromBytes(headerD)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to create header from bytes %s: %v",
			funcName, headerE, err)
		return nil, errs.MsgError(errs.HeaderInvalid, desc)
	}
	return &header, nil
}

// GenerateSolvedBlockHeader create a block header from a mining.submit message
// and its associated job.
func GenerateSolvedBlockHeader(headerE string, extraNonce1E string,
	extraNonce2E string, nTimeE string, nonceE string, miner string) (*wire.BlockHeader, error) {
	headerEB := []byte(headerE)

	switch miner {
	case CPU:
		copy(headerEB[272:280], []byte(nTimeE))
		copy(headerEB[280:288], []byte(nonceE))
		copy(headerEB[288:296], []byte(extraNonce1E))
		copy(headerEB[296:304], []byte(extraNonce2E))

	// The Obelisk DCR1 does not respect the extraNonce2Size specified in the
	// mining.subscribe response sent to it. It returns a 4-byte extraNonce2
	// regardless of the extraNonce2Size provided.
	// The extraNonce2 value submitted is exclusively the extraNonce2.
	// The nTime and nonce values submitted are big endian, they have to
	// be reversed to little endian before header reconstruction.
	case ObeliskDCR1:
		nTimeERev, err := hexReversed(nTimeE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[272:280], []byte(nTimeERev))

		nonceERev, err := hexReversed(nonceE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[280:288], []byte(nonceERev))

		copy(headerEB[288:296], []byte(extraNonce1E))
		copy(headerEB[296:304], []byte(extraNonce2E))

	// The Antiminer DR3 and DR5 return a 12-byte entraNonce comprised of the
	// extraNonce1 and extraNonce2 regardless of the extraNonce2Size specified
	// in the mining.subscribe message. The nTime and nonce values submitted are
	// big endian, they have to be reversed before block header reconstruction.
	case AntminerDR3, AntminerDR5:
		nTimeERev, err := hexReversed(nTimeE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[272:280], []byte(nTimeERev))

		nonceERev, err := hexReversed(nonceE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[280:288], []byte(nonceERev))
		copy(headerEB[288:312], []byte(extraNonce2E))

	// The Innosilicon D9 respects the extraNonce2Size specified in the
	// mining.subscribe response sent to it. The extraNonce2 value submitted is
	// exclusively the extraNonce2. The nTime and nonce values submitted are
	// big endian, they have to be reversed to little endian before header
	// reconstruction.
	case InnosiliconD9:
		nTimeERev, err := hexReversed(nTimeE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[272:280], []byte(nTimeERev))

		nonceERev, err := hexReversed(nonceE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[280:288], []byte(nonceERev))
		copy(headerEB[288:296], []byte(extraNonce1E))
		copy(headerEB[296:304], []byte(extraNonce2E))

	// The Whatsminer D1 does not respect the extraNonce2Size specified in the
	// mining.subscribe response sent to it. The 8-byte extranonce submitted is
	// for the extraNonce1 and extraNonce2. The nTime and nonce values submitted
	// are big endian, they have to be reversed to little endian before header
	// reconstruction.
	case WhatsminerD1:
		nTimeERev, err := hexReversed(nTimeE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[272:280], []byte(nTimeERev))

		nonceERev, err := hexReversed(nonceE)
		if err != nil {
			return nil, err
		}
		copy(headerEB[280:288], []byte(nonceERev))
		copy(headerEB[288:304], []byte(extraNonce2E))

	default:
		desc := fmt.Sprintf("miner %s is unknown", miner)
		return nil, errs.MsgError(errs.MinerUnknown, desc)
	}

	solvedHeaderD, err := hex.DecodeString(string(headerEB))
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode solved header: %v",
			miner, err)
		return nil, errs.MsgError(errs.Decode, desc)
	}

	var solvedHeader wire.BlockHeader
	err = solvedHeader.FromBytes(solvedHeaderD)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to create header from bytes: %v",
			miner, err)
		return nil, errs.MsgError(errs.Parse, desc)
	}

	return &solvedHeader, nil
}

// SubmitWorkRequest creates a submit request message.
func SubmitWorkRequest(id *uint64, workerName string, jobID string, extraNonce2 string, nTime string, nonce string) *Request {
	return &Request{
		ID:     id,
		Method: Submit,
		Params: []string{workerName, jobID, extraNonce2, nTime, nonce},
	}
}

// ParseSubmitWorkRequest resolves a submit work request into its components.
func ParseSubmitWorkRequest(req *Request, miner string) (string, string, string, string, string, error) {
	const funcName = "ParseSubmitWorkRequest"
	if req.Method != Submit {
		desc := fmt.Sprintf("%s: invalid method %q from %q",
			funcName, req.Method, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse submit work parameters from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	if len(params) < 5 {
		desc := fmt.Sprintf("%s: expected 5 submit work "+
			"parameters, got %d from %q", funcName, len(params), miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	workerName, ok := params[0].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse workerName parameter from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	jobID, ok := params[1].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse jobID parameter from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	extraNonce2, ok := params[2].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse extraNonce2 parameter from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	nTime, ok := params[3].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse nTime parameter from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	nonce, ok := params[4].(string)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse nonce parameter from %q",
			funcName, miner)
		return "", "", "", "", "", errs.MsgError(errs.Parse, desc)
	}

	return workerName, jobID, extraNonce2, nTime, nonce, nil
}

// SubmitWorkResponse creates a submit response.
func SubmitWorkResponse(id uint64, status bool, err *StratumError) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: status,
	}
}

// ParseSubmitWorkResponse resolves a submit response into its components.
func ParseSubmitWorkResponse(resp *Response) (bool, *StratumError, error) {
	const funcName = "ParseSubmitWorkResponse"
	status, ok := resp.Result.(bool)
	if !ok {
		desc := fmt.Sprintf("%s: unable to parse result parameter", funcName)
		return false, nil, errs.MsgError(errs.Parse, desc)
	}

	return status, resp.Error, nil
}
