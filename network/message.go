package network

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/dnldd/dcrpool/dividend"
)

// Message types.
const (
	RequestType      = "request"
	ResponseType     = "response"
	NotificationType = "notification"
)

// Handler types.
const (
	Authorize     = "mining.authorize"
	Subscribe     = "mining.subscribe"
	SetDifficulty = "mining.set_difficulty"
	Notify        = "mining.notify"
	Submit        = "mining.submit"
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
	ExtraNonce2Size = 8
)

// NewStratumError creates a stratum error instance.
func NewStratumError(code uint64) []interface{} {
	errData := make([]interface{}, 3)
	errData[0] = code
	errData[2] = nil

	switch code {
	case StaleJob:
		errData[1] = "Stale Job"
	case DuplicateShare:
		errData[1] = "Duplicate share"
	case LowDifficultyShare:
		errData[1] = "Low difficulty share"
	case UnauthorizedWorker:
		errData[1] = "Unauthorized worker"
	case NotSubscribed:
		errData[1] = "Not subscribed"
	default:
		errData[1] = "Other/Unknown"
	}

	return errData
}

// ParseStratumError resolves a stratum error into its components.
func ParseStratumError(err []interface{}) (uint64, string, error) {
	if err == nil {
		return 0, "", fmt.Errorf("nil stratum error provided")
	}

	errCode, ok := err[0].(float64)
	if !ok {
		return 0, "", fmt.Errorf("failed to parse error code parameter")
	}

	code := uint64(errCode)

	message, ok := err[1].(string)
	if !ok {
		return 0, "", fmt.Errorf("failed to parse message parameter")
	}

	return code, message, nil
}

// Message defines a message interface.
type Message interface {
	MessageType() string
}

// Request defines a request message.
type Request struct {
	ID     *uint64     `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// MessageType returns the request message type.
func (req *Request) MessageType() string {
	return RequestType
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
	ID     *uint64       `json:"id"`
	Error  []interface{} `json:"error,omitempty"`
	Result interface{}   `json:"result,omitempty"`
}

// MessageType returns the response message type.
func (req *Response) MessageType() string {
	return ResponseType
}

// NewResponse creates a response instance.
func NewResponse(id *uint64, result interface{}, err []interface{}) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: result,
	}
}

// IdentifyMessage determines the received message type. It returns the message
// cast to the appropriate message type, the message type and an error type.
func IdentifyMessage(data []byte) (Message, string, error) {
	var req Request
	err := json.Unmarshal(data, &req)
	if err != nil {
		return nil, "", err
	}

	if req.Method != "" {
		if req.ID == nil {
			return &req, NotificationType, nil
		}
		return &req, RequestType, nil
	}

	var resp Response
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, "", err
	}

	return &resp, ResponseType, nil
}

// AuthorizeRequest creates an authorize request message.
func AuthorizeRequest(id *uint64, name string, address string) *Request {
	user := fmt.Sprintf("%s.%s", address, name)
	return &Request{
		ID:     id,
		Method: Authorize,
		Params: []string{user, ""},
	}
}

// ParseAuthorizeRequest resolves an authorize request into its components.
func ParseAuthorizeRequest(req *Request) (string, error) {
	if req.Method != Authorize {
		return "", fmt.Errorf("request method is not authorize")
	}

	auth, ok := req.Params.([]interface{})
	if !ok {
		return "", fmt.Errorf("failed to parse authorize parameters")
	}

	username, ok := auth[0].(string)
	if !ok {
		return "", fmt.Errorf("failed to parse username parameter")
	}

	return username, nil
}

// AuthorizeResponse creates an authorize response.
func AuthorizeResponse(id *uint64, status bool, err []interface{}) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: status,
	}
}

// ParseAuthorizeResponse resolves an authorize response into its components.
func ParseAuthorizeResponse(resp *Response) (bool, []interface{}, error) {
	status, ok := resp.Result.(bool)
	if !ok {
		return false, nil, fmt.Errorf("failed to parse result parameter")
	}

	return status, resp.Error, nil
}

// SubscribeRequest creates a subscribe request message.
func SubscribeRequest(id *uint64) *Request {
	return &Request{
		ID:     id,
		Method: Subscribe,
		Params: []string{},
	}
}

// ParseSubscribeRequest resolves a subscribe request into its components.
func ParseSubscribeRequest(req *Request) error {
	if req.Method != Subscribe {
		return fmt.Errorf("request method is not subscribe")
	}

	return nil
}

// SubscribeResponse creates a subscribe response.
func SubscribeResponse(id *uint64, extraNonce1 string, err []interface{}) *Response {
	if err != nil {
		return &Response{
			ID:     id,
			Error:  err,
			Result: nil,
		}
	}

	// TODO: set the difficulty and notify ids when it's supported.
	return &Response{
		ID:    id,
		Error: nil,
		Result: []interface{}{[][]string{
			{"mining.set_difficulty", "not supported"},
			{"mining.notify", "not supported"}},
			extraNonce1, ExtraNonce2Size},
	}
}

// ParseSubscribeResponse resolves a subscribe response into its components.
func ParseSubscribeResponse(resp *Response) (string, string, string, uint64, error) {
	if resp.Error != nil {
		return "", "", "", 0, fmt.Errorf("%s", resp.Error)
	}

	res, ok := resp.Result.([]interface{})
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse result parameter")
	}

	subs, ok := res[0].([]interface{})
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse subscription details")
	}

	diff, ok := subs[0].([]interface{})
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse difficulty id details")
	}

	diffID, ok := diff[1].(string)
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse difficulty id")
	}

	notify, ok := subs[1].([]interface{})
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse notify id details")
	}

	notifyID, ok := notify[1].(string)
	if !ok {
		return "", "", "", 0, fmt.Errorf("failed to parse notify id")
	}

	extraNonce1, ok := res[1].(string)
	if !ok {
		return "", "", "", 0,
			fmt.Errorf("failed to parse ExtraNonce1 parameter")
	}

	nonce2Size, ok := res[2].(float64)
	if !ok {
		return "", "", "", 0,
			fmt.Errorf("failed to parse ExtraNonce2Size parameter")
	}

	extraNonce2Size := uint64(nonce2Size)

	return diffID, notifyID, extraNonce1, extraNonce2Size, nil
}

// SetDifficultyNotification creates a set difficulty notification message.
func SetDifficultyNotification(net *chaincfg.Params, target uint32) (*Request, error) {
	difficulty, err := dividend.TargetToDifficulty(net, target)
	if err != nil {
		return nil, err
	}
	return &Request{
		Method: SetDifficulty,
		Params: []uint64{difficulty},
	}, nil
}

// ParseSetDifficultyNotification resolves a set difficulty notification into
// its components.
func ParseSetDifficultyNotification(req *Request) (uint64, error) {
	if req.Method != SetDifficulty {
		return 0, fmt.Errorf("notification method is not set difficulty")
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		return 0, fmt.Errorf("failed to parse set difficulty parameters")
	}

	return uint64(params[0].(float64)), nil
}

// WorkNotification creates a work notification message.
func WorkNotification(jobID string, prevBlock string, genTx1 string, genTx2 string, blockVersion string, nTime string, cleanJob bool) *Request {
	return &Request{
		Method: Notify,
		Params: []interface{}{jobID, prevBlock, genTx1, genTx2, []string{},
			blockVersion, "", nTime, cleanJob},
	}
}

// ParseWorkNotification resolves a work notification message into its components.
func ParseWorkNotification(req *Request) (string, string, string, string, string, string, bool, error) {
	if req.Method != Notify {
		return "", "", "", "", "", "", false,
			fmt.Errorf("notification method is not notify")
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse work parameters")
	}

	jobID, ok := params[0].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse jobID parameter")
	}

	prevBlock, ok := params[1].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse prevBlock parameter")
	}

	genTx1, ok := params[2].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse genTx1 parameter")
	}

	genTx2, ok := params[3].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse genTx2 parameter")
	}

	blockVersion, ok := params[5].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse blockVersion parameter")
	}

	nTime, ok := params[7].(string)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse nTime parameter")
	}

	cleanJob, ok := params[8].(bool)
	if !ok {
		return "", "", "", "", "", "", false,
			fmt.Errorf("failed to parse cleanJob parameter")
	}

	return jobID, prevBlock, genTx1, genTx2, blockVersion, nTime, cleanJob, nil
}

// GenerateBlockHeader creates a block header from a mining.notify
// message and the extraNonce1 of the client.
func GenerateBlockHeader(blockVersionE string, prevBlockE string, genTx1E string, nTimeE string, extraNonce1E string, genTx2E string) (*wire.BlockHeader, error) {
	buf := bytes.NewBufferString("")
	buf.WriteString(blockVersionE)
	buf.WriteString(prevBlockE)
	buf.WriteString(genTx1E)
	buf.WriteString(nTimeE)
	buf.WriteString(strings.Repeat("0", 8))
	buf.WriteString(extraNonce1E)
	buf.WriteString(strings.Repeat("0", 56))
	buf.WriteString(genTx2E)
	headerE := buf.String()

	headerD, err := hex.DecodeString(headerE)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block header: %v", err)
	}

	var header wire.BlockHeader
	err = header.FromBytes(headerD)
	if err != nil {
		return nil, err
	}

	return &header, nil
}

// GenerateSolvedBlockHeader create a block header from a mining.submit message
// and its associated job. It returns the solved block header and the entire
// nonce space.
func GenerateSolvedBlockHeader(headerE string, extraNonce1E string, extraNonce2E string, nTimeE string, nonceE string) (*wire.BlockHeader, []byte, error) {
	buf := bytes.NewBufferString("")
	buf.WriteString(headerE[:272])
	buf.WriteString(nTimeE)
	buf.WriteString(nonceE)
	buf.WriteString(extraNonce1E)
	buf.WriteString(extraNonce2E)
	buf.WriteString(headerE[304:])
	solvedHeaderE := buf.String()

	solvedHeaderD, err := hex.DecodeString(solvedHeaderE)
	if err != nil {
		return nil, nil,
			fmt.Errorf("failed to decode solved block header: %v", err)
	}

	var solvedHeader wire.BlockHeader
	err = solvedHeader.FromBytes(solvedHeaderD)
	if err != nil {
		return nil, nil,
			fmt.Errorf("failed to create block header from bytes:%v", err)
	}

	nonceSpace := solvedHeaderD[140:152]

	return &solvedHeader, nonceSpace, nil
}

// SubmitWorkRequest creates a submit request message.
func SubmitWorkRequest(id *uint64, workerName string, jobID string, extraNonce2 string, nTime string, nonce string) *Request {
	return &Request{
		ID:     id,
		Method: Submit,
		Params: []string{workerName, jobID, extraNonce2, nTime, nonce},
	}
}

// HexReversed reverses a hex string.
func HexReversed(in string) (string, error) {
	if len(in)%2 != 0 {
		return "", fmt.Errorf("incorrect hex input length")
	}

	buf := bytes.NewBufferString("")
	for i := len(in) - 1; i > -1; i -= 2 {
		buf.WriteByte(in[i-1])
		buf.WriteByte(in[i])
	}
	return buf.String(), nil
}

// ParseSubmitWorkRequest resolves a submit work request into its components.
func ParseSubmitWorkRequest(req *Request, miner string) (string, string, string, string, string, error) {
	if req.Method != Submit {
		return "", "", "", "", "", fmt.Errorf("request method is not submit")
	}

	params, ok := req.Params.([]interface{})
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse submit work parameters")
	}

	workerName, ok := params[0].(string)
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse workerName parameter")
	}

	jobID, ok := params[1].(string)
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse jobID parameter")
	}

	extraNonce2, ok := params[2].(string)
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse extraNonce2 parameter")
	}

	nTime, ok := params[3].(string)
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse nTime parameter")
	}

	switch miner {
	// The miners listed submit nTime as a hex encoded big endian, the
	// bytes have to the reversed to little endian to proceed.
	case dividend.InnosiliconD9, dividend.ObeliskDCR1, dividend.AntminerDR3,
		dividend.StrongUU1, dividend.AntminerDR5:
		rev, err := HexReversed(nTime)
		if err != nil {
			return "", "", "", "", "", err
		}
		nTime = rev
	}

	nonce, ok := params[4].(string)
	if !ok {
		return "", "", "", "", "",
			fmt.Errorf("failed to parse nonce parameter")
	}

	return workerName, jobID, extraNonce2, nTime, nonce, nil
}

// SubmitWorkResponse creates a submit response.
func SubmitWorkResponse(id *uint64, status bool, err []interface{}) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: status,
	}
}

// ParseSubmitWorkResponse resolves a submit response into its components.
func ParseSubmitWorkResponse(resp *Response) (bool, []interface{}, error) {
	status, ok := resp.Result.(bool)
	if !ok {
		return false, nil, fmt.Errorf("failed to parse result parameter")
	}

	return status, resp.Error, nil
}
