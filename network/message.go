package network

import (
	"encoding/json"
	"errors"
)

// Message types.
const (
	RequestType      = "request"
	ResponseType     = "response"
	NotificationType = "notification"
)

// Thresholds.
const (
	MaxMessageSize = 1024
	MaxPingRetries = 3
)

// Handler types.
const (
	Ping              = "ping"
	Pong              = "pong"
	Work              = "work"
	SubmitWork        = "submitwork"
	EvaluatedWork     = "evaluatedwork"
	ConnectedBlock    = "connectedblock"
	DisconnectedBlock = "disconnectedblock"
)

// Message is the base interface messages exchanged between a websocket client
// and the server must adhere to.
type Message interface {
	HasID() bool
}

// Request defines a request message. It specifies the targetted processing
// method and supplies the required parameters.
type Request struct {
	ID     *uint64                `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// HasID determines if the request has an ID.
func (req *Request) HasID() bool {
	return req.ID != nil
}

// Response defines a response message. It bundles the payload
// of the preceding request and any errors in processing the request, if any.
type Response struct {
	ID     *uint64                `json:"id"`
	Error  *string                `json:"error,omitempty"`
	Result map[string]interface{} `json:"result,omitempty"`
}

// HasID determines if the response has an ID.
func (resp *Response) HasID() bool {
	return resp.ID != nil
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
		if !req.HasID() {
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

// PingRequest is a convenince function for creating ping requests.
func PingRequest(id *uint64) *Request {
	return &Request{
		ID:     id,
		Method: Ping,
		Params: nil,
	}
}

// PongResponse is a convenience function for creating a pong response.
func PongResponse(id *uint64) *Response {
	return &Response{
		ID:     id,
		Error:  nil,
		Result: map[string]interface{}{"response": Pong},
	}
}

// tooManyRequestsResponse is a convenience function for creating
// a TooManyRequests response.
func tooManyRequestsResponse(id *uint64) *Response {
	err := "too many requests"
	return &Response{
		ID:     id,
		Error:  &err,
		Result: nil,
	}
}

// WorkNotification is a convenience function for creating a work notification.
func WorkNotification(header string, target string) *Request {
	return &Request{
		ID:     nil,
		Method: Work,
		Params: map[string]interface{}{"header": header, "target": target},
	}
}

// ParseWorkNotification parses a work notification message.
func ParseWorkNotification(req *Request) ([]byte, []byte, error) {
	header, ok := req.Params["header"].(string)
	if !ok {
		return nil, nil, errors.New("Invalid work notification, 'params' data " +
			"should have 'header' field")
	}

	target, ok := req.Params["target"].(string)
	if !ok {
		return nil, nil, errors.New("Invalid work notification, 'params' data " +
			"should have 'target' field")
	}

	return []byte(header), []byte(target), nil
}

// WorkSubmissionRequest is a convenience function for creating a work
// submision request.
func WorkSubmissionRequest(id *uint64, header string) *Request {
	return &Request{
		ID:     id,
		Method: SubmitWork,
		Params: map[string]interface{}{"header": header},
	}
}

// ParseWorkSubmissionRequest parses a work submission request.
func ParseWorkSubmissionRequest(req *Request) (*string, error) {
	header, ok := req.Params["header"].(string)
	if !ok {
		return nil, errors.New("Invalid work submission request, " +
			"'params' data should have 'header' field")
	}

	return &header, nil
}

// EvaluatedWorkResponse is a convenience function for creating an evaluated
// work response.
func EvaluatedWorkResponse(id *uint64, err *string, status bool) *Response {
	return &Response{
		ID:     id,
		Error:  err,
		Result: map[string]interface{}{"accepted": status},
	}
}

// ParseEvaluatedWorkResponse parses an evaluated work response message.
func ParseEvaluatedWorkResponse(resp *Response) (bool, error) {
	accepted, ok := resp.Result["accepted"].(bool)
	if !ok {
		return false, errors.New("Invalid evaluated work response, " +
			"'result' data should have 'accepted' field")
	}

	if resp.Error != nil {
		return accepted, errors.New(*resp.Error)
	}

	return accepted, nil
}

// ConnectedBlockNotification is a convenience function for creating a
// connected block notification.
func ConnectedBlockNotification(blkHeight uint32) *Request {
	return &Request{
		ID:     nil,
		Method: ConnectedBlock,
		Params: map[string]interface{}{"height": blkHeight},
	}
}

// ParseConnectedBlockNotification parses a connected block notification
// message.
func ParseConnectedBlockNotification(req *Request) (uint32, error) {
	height, ok := req.Params["height"].(float64)
	if !ok {
		return 0, errors.New("Invalid connected block notification, " +
			"'params' data should have 'height' field")
	}

	return uint32(height), nil
}

// DisconnectedBlockNotification is a convenience function for creating a
// disconnected block notification.
func DisconnectedBlockNotification(blkHeight uint32) *Request {
	return &Request{
		ID:     nil,
		Method: DisconnectedBlock,
		Params: map[string]interface{}{"height": blkHeight},
	}
}

// ParseDisconnectedBlockNotification parses a disconnected block notification
// message.
func ParseDisconnectedBlockNotification(req *Request) (uint32, error) {
	height, ok := req.Params["height"].(float64)
	if !ok {
		return 0, errors.New("Invalid disconnected block notification, " +
			"'params' data should have 'height' field")
	}

	return uint32(height), nil
}
