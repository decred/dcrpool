package ws

const (
	ping = "ping"
	pong = "pong"
)

var (
	pingReq  = pingRequest()
	pongResp = pongResponse()
)

// Message defines the Haste message interface.
type Message interface {
	GetID() *uint
}

// Request defines a Haste websocket request message. It specifies the
// targetted processing method and supplies the required parameters
// for the targetted method.
type Request struct {
	ID     *uint       `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// GetID fetches the ID of the request.
func (req *Request) GetID() *uint {
	return req.ID
}

// Response defines a Haste websocket response message. It bundles the payload
// of the preceding request and any errors in processing the request, if any.
type Response struct {
	ID     *uint       `json:"id"`
	Error  *string     `json:"error"`
	Result interface{} `json:"interface"`
}

// GetID fetches the ID of the request.
func (resp *Response) GetID() *uint {
	return resp.ID
}

// pingRequest creates a ping request.
func pingRequest() *Request {
	return &Request{
		ID:     nil,
		Method: "ping",
		Params: nil,
	}
}

// pongResponse creates a pong response.
func pongResponse() *Response {
	return &Response{
		ID:     nil,
		Error:  nil,
		Result: "pong",
	}
}
