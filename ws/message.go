package ws

import (
	"encoding/json"
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
	Ping = "ping"
	Pong = "pong"
)

// Message is the base interface messages exchanged between a websocket client
// and the server must adhere to.
type Message interface {
	HasID() bool
}

// Request defines a request message. It specifies the targetted processing
// method and supplies the required parameters.
type Request struct {
	ID     *uint64     `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// HasID determines if the request has an ID.
func (req *Request) HasID() bool {
	return req.ID != nil
}

// Response defines a response message. It bundles the payload
// of the preceding request and any errors in processing the request, if any.
type Response struct {
	ID     *uint64     `json:"id"`
	Error  *string     `json:"error"`
	Result interface{} `json:"interface"`
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

	if !req.HasID() {
		return &resp, NotificationType, nil
	}
	return &resp, ResponseType, nil
}

// PingRequest is a convenince function for creating ping requests.
func PingRequest(id *uint64) *Request {
	return &Request{
		ID:     id,
		Method: "ping",
		Params: nil,
	}
}

// PongResponse is a convenience function for creating a pong response.
func PongResponse(id *uint64) *Response {
	return &Response{
		ID:     id,
		Error:  nil,
		Result: "pong",
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
		Method: "work",
		Params: map[string]string{"header": header, "target": target},
	}
}
