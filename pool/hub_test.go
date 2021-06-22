// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"decred.org/dcrwallet/rpc/walletrpc"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type tRescanClient struct {
	resp *walletrpc.RescanResponse
	err  error
	grpc.ClientStream
}

func (r *tRescanClient) Recv() (*walletrpc.RescanResponse, error) {
	return r.resp, r.err
}

func (r *tRescanClient) Header() (metadata.MD, error) {
	return nil, nil
}

func (r *tRescanClient) Trailer() metadata.MD {
	return nil
}

func (r *tRescanClient) CloseSend() error {
	return nil
}

func (r *tRescanClient) Context() context.Context {
	return nil
}

func (r *tRescanClient) SendMsg(m interface{}) error {
	return nil
}

func (r *tRescanClient) RecvMsg(m interface{}) error {
	return nil
}

type tWalletConnection struct {
}

func (t *tWalletConnection) Balance(_ context.Context, in *walletrpc.BalanceRequest, _ ...grpc.CallOption) (*walletrpc.BalanceResponse, error) {
	if in.AccountNumber != 69 {
		return nil, errors.New("expected Balance to be called with AccountNumber=69, as defined in hub config")
	}
	return &walletrpc.BalanceResponse{
		Total:     90009989240,
		Spendable: 90009989240,
	}, nil
}

func (t *tWalletConnection) ConstructTransaction(_ context.Context, in *walletrpc.ConstructTransactionRequest, _ ...grpc.CallOption) (*walletrpc.ConstructTransactionResponse, error) {
	if in.SourceAccount != 69 {
		return nil, errors.New("expected ConstructTransaction to be called with SourceAccount=69, as defined in hub config")
	}
	unsignedTx, err := hex.DecodeString("010000000432e2698697e10772e4e98e994089d" +
		"bcd444f65638c770419cdbc5ba53d9581c80000000000ffffffff7739bf88638c5f3" +
		"0cae3423f6052d37d5dccc016d22ee8dc0c889f0c112927f10200000000ffffffffc" +
		"896885e2bc29b75c5c8255e7abb7fb66651b0a6696a0c7ebcbe7b5ad1e134df02000" +
		"00000ffffffffe0dbac616b74d8b8eaf16868ffaf84cde8b2c526e593d57289369d3" +
		"a076b00df0200000000ffffffff03ba4d98000000000000001976a914583da28e0ac" +
		"bffd5a840770d9bb29375619e285f88ac00e9a4350000000000001976a9140b6bfcc" +
		"e946d4ba887ada3898b086812b65b7b8d88ac001bc6be1400000000001976a91439c" +
		"615144c3b8c777d7d0f99740e98c9fca0c04d88ac000000000000000004786c98000" +
		"000000000000000ffffffff0000ac23fc0600000000000000ffffffff0000ac23fc0" +
		"600000000000000ffffffff0000ac23fc0600000000000000ffffffff00")
	if err != nil {
		return nil, err
	}

	return &walletrpc.ConstructTransactionResponse{
		UnsignedTransaction:       unsignedTx,
		TotalPreviousOutputAmount: 90009989240,
		TotalOutputAmount:         90009981370,
		EstimatedSignedSize:       787,
	}, nil
}

func (t *tWalletConnection) SignTransaction(context.Context, *walletrpc.SignTransactionRequest, ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error) {
	signedTx, err := hex.DecodeString("010000000432e2698697e10772e4e98e994089" +
		"dbcd444f65638c770419cdbc5ba53d9581c80000000000ffffffff7739bf88638c5f30ca" +
		"e3423f6052d37d5dccc016d22ee8dc0c889f0c112927f10200000000ffffffffc896885e" +
		"2bc29b75c5c8255e7abb7fb66651b0a6696a0c7ebcbe7b5ad1e134df0200000000ffffff" +
		"ffe0dbac616b74d8b8eaf16868ffaf84cde8b2c526e593d57289369d3a076b00df020000" +
		"0000ffffffff03ba4d98000000000000001976a914583da28e0acbffd5a840770d9bb293" +
		"75619e285f88ac00e9a4350000000000001976a9140b6bfcce946d4ba887ada3898b0868" +
		"12b65b7b8d88ac001bc6be1400000000001976a91439c615144c3b8c777d7d0f99740e98" +
		"c9fca0c04d88ac000000000000000004786c98000000000000000000ffffffff6a473044" +
		"02202a09ef79350424f081f82107d15f32fd9adc8b9898f777d465b013b9f12c8c520220" +
		"59023c7064b630d2b2aad593d90b0875f3a9d1d9f7892c00fa6086d6c3045672012102eb" +
		"e7dc0e3231546442405209720d464ec4cb8fc4c6c40f1dd64d20b7f541f7a800ac23fc06" +
		"00000000000000ffffffff6a47304402201b98f24621c4eb4693866c810bb51f45b355ba" +
		"992dec07875cc8efbd138c55ac022026361f419f8dc69120d087fd25febb3648c1587961" +
		"b79d65e9fcce1b405933f20121028b85616443f420a39500ff0bea67d52181e949e5203f" +
		"24413ec0d959758d786d00ac23fc0600000000000000ffffffff6a473044022058df7f5c" +
		"7f6f92e928176486f0de7668beaedcbc12e12af8a89c882e4882cbaa02207c3fadea727a" +
		"3b227b03a9aa2595c640eea41f8c543c98e173e8d845f2df7fe80121028b85616443f420" +
		"a39500ff0bea67d52181e949e5203f24413ec0d959758d786d00ac23fc06000000000000" +
		"00ffffffff6b483045022100a3b55ef1c1e507f0611ea60a3fec85cddd985215970c1d0c" +
		"afe9d976418765bd02202c87f0dd11164b8ad6538f33defffb5d00a2cd84908ebdca6126" +
		"ff832a3e71f80121028b85616443f420a39500ff0bea67d52181e949e5203f24413ec0d9" +
		"59758d786d")
	if err != nil {
		return nil, err
	}

	return &walletrpc.SignTransactionResponse{
		Transaction: signedTx,
	}, nil
}

func (t *tWalletConnection) PublishTransaction(context.Context, *walletrpc.PublishTransactionRequest, ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error) {
	txHash, err := hex.DecodeString("d2a667e0f1643c3deb2de87ef9af3252679459f62" +
		"5451a351b1510b5090abe23")
	if err != nil {
		return nil, err
	}
	return &walletrpc.PublishTransactionResponse{
		TransactionHash: txHash,
	}, nil
}

func (t *tWalletConnection) Rescan(context.Context, *walletrpc.RescanRequest, ...grpc.CallOption) (walletrpc.WalletService_RescanClient, error) {
	client := new(tRescanClient)
	client.resp = &walletrpc.RescanResponse{
		RescannedThrough: 10000,
	}

	return client, nil
}

func (t *tWalletConnection) GetTransaction(context.Context, *walletrpc.GetTransactionRequest, ...grpc.CallOption) (*walletrpc.GetTransactionResponse, error) {
	return &walletrpc.GetTransactionResponse{
		Transaction:   &walletrpc.TransactionDetails{},
		Confirmations: 25,
		BlockHash:     zeroHash[:],
	}, nil
}

type tNodeConnection struct{}

func (t *tNodeConnection) CreateRawTransaction(context.Context, []chainjson.TransactionInput, map[dcrutil.Address]dcrutil.Amount, *int64, *int64) (*wire.MsgTx, error) {
	return nil, nil
}

func (t *tNodeConnection) GetTxOut(context.Context, *chainhash.Hash, uint32, bool) (*chainjson.GetTxOutResult, error) {
	return nil, nil
}

func (t *tNodeConnection) GetWorkSubmit(_ context.Context, sub string) (bool, error) {
	return false, nil
}

func (t *tNodeConnection) GetWork(context.Context) (*chainjson.GetWorkResult, error) {
	return &chainjson.GetWorkResult{
		Data: "07000000ddb9fb70cb6ed184f57bfb94abebe7e7b9819e27d6e3ca8" +
			"19f1f73c7218100007de69dd9365ba5a39178870780d78d86aa6d53a649a5" +
			"4bd65faac4be8123253e7f98f31055b0f3e94dd48e67f43742b028623192d" +
			"d684d053d6681759c8ebfa70100000000000000000000003c00000045bc4d" +
			"20204e00000000000039000000b3060000a912825e0000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000" +
			"8000000100000000000005a0",
		Target: "45bc4d20",
	}, nil
}

func (t *tNodeConnection) GetBlock(_ context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	b57 := []byte("07000000ddb9fb70cb6ed184f57bfb94abebe7e7b9819e27d6e3ca819" +
		"f1f73c7218100007de69dd9365ba5a39178870780d78d86aa6d53a649a54bd65" +
		"faac4be8123253e7f98f31055b0f3e94dd48e67f43742b028623192dd684d053" +
		"d6681759c8ebfa70100000000000000000000003c00000045bc4d20204e00000" +
		"000000039000000b3060000a912825e6efa010015d5e25900000000000000000" +
		"0000000000000000000000000000000000000000000000003010000000100000" +
		"00000000000000000000000000000000000000000000000000000000000fffff" +
		"fff00ffffffff0300f2052a01000000000017a914cbb08d6ca783b533b2c7d24" +
		"a51fbca92d937bf9987000000000000000000000e6a0c3900000016eb634051b" +
		"1b5fc00ac23fc0600000000001976a914dcd4f28811382640a0d357bf88b1490" +
		"5840c46c788ac000000000000000001009e29260800000000000000ffffffff0" +
		"800002f646372642f0100000004ab289dc91533aac497954f58f2ae39233e654" +
		"ab7a3618a4d2be6cfb038ab415b0200000000ffffffffc1e8c460f187bc9a58a" +
		"c75932906251437e11c8564871ab7352ebe52709bb6cb0200000000ffffffffd" +
		"a2c7b58f25cc5fbc6103e776afbde2adfaa6b2c729ff232e7408d35e6b61e110" +
		"200000000fffffffff62a367e29f690e6223cfca7f3b92c040b316dce256d9d5" +
		"ac09aa311b6d50b2f0100000000ffffffff03ba4d98000000000000001976a91" +
		"4583da28e0acbffd5a840770d9bb29375619e285f88ac001bc6be14000000000" +
		"01976a91439c615144c3b8c777d7d0f99740e98c9fca0c04d88ac00e9a435000" +
		"0000000001976a9140b6bfcce946d4ba887ada3898b086812b65b7b8d88ac000" +
		"00000000000000400ac23fc0600000024000000000000006b483045022100bc9" +
		"30094964a3fee0e0cd7d6a2edc30a8e0f3ba17281c2720474e204216245ea022" +
		"03ed6d5de7b61b08d5599d6cb276ad749171776225b82c959cb43075c81bddef" +
		"60121028b85616443f420a39500ff0bea67d52181e949e5203f24413ec0d9597" +
		"58d786d00ac23fc0600000026000000000000006a473044022024b51d2666479" +
		"036ba04954acdc9c73839f774651c9a49aa31419ec3edbea7570220365621b54" +
		"a04c2c10a5ea02b46f6901586196921f28472c8f05daea37757456d0121028b8" +
		"5616443f420a39500ff0bea67d52181e949e5203f24413ec0d959758d786d00a" +
		"c23fc0600000025000000000000006a473044022046403b5cddf56b742fd6e00" +
		"4ce645804dd86e04d563193fc8aaba1bcab415b9002200448e49801ca3104555" +
		"b69469061dd3dfaf7068aca7cfc5204bdbcfa3d5b27c30121028b85616443f42" +
		"0a39500ff0bea67d52181e949e5203f24413ec0d959758d786d786c980000000" +
		"00036000000010000006b483045022100f7ed059c3b1f77666c24bd315658125" +
		"d775324be299e556b010462360851954202200b3b65047f868ff23de6e39749a" +
		"234ac58db4951aa81e9f668774173c2f5c607012102ebe7dc0e3231546442405" +
		"209720d464ec4cb8fc4c6c40f1dd64d20b7f541f7a80100000001541676aad05" +
		"b2bcd073d14073cbb2f32be339dfa5de183da35bfc17deb3021f90200000000f" +
		"fffffff0bc45900000000000000001976a914dbe59322b2dab779fd997f1aa1a" +
		"d437f683abd8f88acc45900000000000000001976a914dbe59322b2dab779fd9" +
		"97f1aa1ad437f683abd8f88acc45900000000000000001976a914dbe59322b2d" +
		"ab779fd997f1aa1ad437f683abd8f88acc45900000000000000001976a914dbe" +
		"59322b2dab779fd997f1aa1ad437f683abd8f88acc4590000000000000000197" +
		"6a914dbe59322b2dab779fd997f1aa1ad437f683abd8f88acc45900000000000" +
		"000001976a914dbe59322b2dab779fd997f1aa1ad437f683abd8f88acc459000" +
		"00000000000001976a914dbe59322b2dab779fd997f1aa1ad437f683abd8f88a" +
		"cc45900000000000000001976a914dbe59322b2dab779fd997f1aa1ad437f683" +
		"abd8f88acc45900000000000000001976a914dbe59322b2dab779fd997f1aa1a" +
		"d437f683abd8f88acc45900000000000000001976a914dbe59322b2dab779fd9" +
		"97f1aa1ad437f683abd8f88acce1320fc0600000000001976a914bb802894b04" +
		"6bb29dae30baf41081922d77d69fe88ac00000000000000000100ac23fc06000" +
		"0001a000000000000006b48304502210099171c4e59c9afd7d0b3a435649e33b" +
		"6a77493ae522f612311308405b4fa16d40220675b8fe24c401261e419e979b6c" +
		"970fe8541f51d0a63b1298da4d959fdf5dbd0012103e269b8dfcfda884410293" +
		"9516c3d099af94743b4e4000f16407e297771aec30e00")

	b57D := make([]byte, hex.DecodedLen(len(b57)))
	_, err := hex.Decode(b57D, b57)
	if err != nil {
		return nil, err
	}

	var b wire.MsgBlock
	err = b.FromBytes(b57D)
	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (t *tNodeConnection) GetBlockVerbose(context.Context, *chainhash.Hash, bool) (*chainjson.GetBlockVerboseResult, error) {
	return &chainjson.GetBlockVerboseResult{
		Confirmations: -1,
	}, nil
}
func (t *tNodeConnection) NotifyWork(context.Context) error {
	return nil
}

func (t *tNodeConnection) NotifyBlocks(context.Context) error {
	return nil
}

func (t *tNodeConnection) Shutdown() {}

func testHub(t *testing.T) {
	activeNet := chaincfg.SimNetParams()
	powLimit := chaincfg.SimNetParams().PowLimit
	powLimitF, _ := new(big.Float).SetInt(powLimit).Float64()
	iterations := math.Pow(2, 256-math.Floor(math.Log2(powLimitF)))
	hcfg := &HubConfig{
		ActiveNet:             activeNet,
		DB:                    db,
		PoolFee:               0.1,
		LastNPeriod:           time.Second * 120,
		SoloPool:              false,
		PaymentMethod:         PPS,
		MaxGenTime:            time.Second * 20,
		PoolFeeAddrs:          []dcrutil.Address{poolFeeAddrs},
		MaxConnectionsPerHost: 10,
		NonceIterations:       iterations,
		MinerListen:           "127.0.0.1:5050",
		WalletAccount:         69,
		MonitorCycle:          time.Minute,
		MaxUpgradeTries:       5,
	}
	ctx, cancel := context.WithCancel(context.Background())
	hub, err := NewHub(cancel, hcfg)
	if err != nil {
		t.Fatalf("[NewHub] unexpected error: %v", err)
	}

	notifHandlers := hub.createNotificationHandlers()
	if notifHandlers == nil {
		t.Fatalf("[CreatNotificationHandlers] expected an "+
			"initialized notifications handler: %v", err)
	}

	// Create dummy wallet and node connections.
	nodeConn := &tNodeConnection{}
	hub.nodeConn = nodeConn
	walletConn := &tWalletConnection{}
	walletClose := func() error {
		return nil
	}

	hub.walletConn = walletConn
	hub.walletClose = walletClose

	err = hub.FetchWork(ctx)
	if err != nil {
		t.Fatalf("[FetchWork] unexpected error: %v", err)
	}

	// Ensure work quotas are generated as expected.
	now := time.Now()
	tenBefore := now.Add(-(time.Second * 10)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	sixtyAfter := now.Add(time.Second * 60).UnixNano()
	xWeight := new(big.Rat).SetFloat64(1.0)
	yWeight := new(big.Rat).SetFloat64(4.0)
	err = persistShare(db, xID, xWeight, sixtyAfter)
	if err != nil {
		t.Fatal(err)
	}
	err = persistShare(db, xID, xWeight, tenBefore)
	if err != nil {
		t.Fatal(err)
	}
	err = persistShare(db, yID, yWeight, thirtyBefore)
	if err != nil {
		t.Fatal(err)
	}

	quotas, err := hub.FetchWorkQuotas()
	if err != nil {
		t.Fatalf("[FetchWorkQuotas] unexpected error: %v", err)
	}
	if len(quotas) != 2 {
		t.Fatalf("expected a work quota length of 2, got %v", len(quotas))
	}

	var xQuota, yQuota *Quota
	for _, q := range quotas {
		if q.AccountID == xID {
			xQuota = q
		}
		if q.AccountID == yID {
			yQuota = q
		}
	}

	sum := new(big.Rat).Add(yQuota.Percentage, xQuota.Percentage)
	if sum.Cmp(new(big.Rat).SetInt(new(big.Int).SetInt64(1))) != 0 {
		t.Fatalf("expected the sum of share percentages to be 1, got %v", sum)
	}

	// Ensure account x's work quota is four times less than
	// account y's work quota.
	xPercent, _ := xQuota.Percentage.Float64()
	yPercent, _ := yQuota.Percentage.Float64()
	if yPercent/xPercent < 4 {
		t.Fatal("expected account y's quota to be four times more" +
			" than account x's work quota")
	}

	go hub.Run(ctx)

	// Create the mined work to be confirmed.
	work := NewAcceptedWork(
		"00008121c7731f9f81cae3d6279e81b9e7e7ebab94fb7bf584d16ecb70fbb9dd",
		"000033925cfb136f209b2722c4149dd53fceb0323f74b39be753887c19edcd2c",
		56,
		"193c4b8fd02aaed33ab9c5418ace9bec4047f61f923767bceb5a51c6e368bfa6",
		CPU)

	err = db.persistAcceptedWork(work)
	if err != nil {
		t.Fatalf("[Persist] unexpected error: %v", err)
	}

	port := uint32(5030)
	laddr, err := net.ResolveTCPAddr("tcp",
		fmt.Sprintf("%s:%d", "127.0.0.1", port))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer ln.Close()
	serverCh := make(chan net.Conn)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				var opErr *net.OpError
				if errors.As(err, &opErr) {
					if opErr.Op == "accept" {
						if strings.Contains(opErr.Err.Error(),
							"use of closed network connection") {
							return
						}
					}
				}
				log.Errorf("unable to accept connection %v", err)
				return
			}
			serverCh <- conn
		}
	}()

	// Make a CPU client connection.
	conn, srvr, err := makeConn(ln, serverCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer conn.Close()
	defer srvr.Close()

	msgA := &connection{
		Conn: conn,
		Done: make(chan bool),
	}
	hub.endpoint.connCh <- msgA
	<-msgA.Done

	// Ensure the hub has a connected client.
	if !hub.HasClients() {
		t.Fatal("expected hub to have connected clients")
	}

	// Force subscribe and authorize connected clients to allow
	// receiving work notifications.
	hub.endpoint.clientsMtx.Lock()
	for _, client := range hub.endpoint.clients {
		client.authorized = true
		client.subscribed = true
	}
	hub.endpoint.clientsMtx.Unlock()

	// Ensure the hub can process and distribute work to connected clients.
	workE := "07000000ddb9fb70cb6ed184f57bfb94abebe7e7b9819e27d6e3ca8" +
		"19f1f73c7218100007de69dd9365ba5a39178870780d78d86aa6d53a649a5" +
		"4bd65faac4be8123253e7f98f31055b0f3e94dd48e67f43742b028623192d" +
		"d684d053d6681759c8ebfa70100000000000000000000003c00000045bc4d" +
		"20204e00000000000039000000b3060000a912825e0000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000" +
		"8000000100000000000005a0"
	hub.processWork(workE)

	// Get a block and publish a bogus transaction by confirming the
	// mined accepted work.
	hub.chainState.setCurrentWork(workE)
	headerB, err := hex.DecodeString(workE[:360])
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}
	var header wire.BlockHeader
	err = header.FromBytes(headerB)
	if err != nil {
		t.Fatalf("unexpected deserialization error: %v", err)
	}
	headerB, err = header.Bytes()
	if err != nil {
		t.Fatalf("unexpected serialization error: %v", err)
	}
	confNotif := &blockNotification{
		Header: headerB,
		Done:   make(chan bool),
	}
	hub.chainState.connCh <- confNotif
	<-confNotif.Done

	// Ensure the hub can process submitted work.
	_, err = hub.submitWork(ctx, &workE)
	if err != nil {
		t.Fatalf("unexpected submit work error: %v", err)
	}

	// Create an account.
	account := NewAccount(xAddr)
	err = db.persistAccount(account)
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	// Ensure hub can check for account existence.
	if !hub.AccountExists(account.UUID) {
		t.Fatalf("expected account with id %s to exist", account.UUID)
	}

	// Ensure the gui CSRF secret can be generated.
	csrf, err := hub.CSRFSecret()
	if err != nil {
		t.Fatalf("[CSRFSecret] unexpected error: %v", err)
	}
	if csrf == nil {
		t.Fatal("expected a non-nil csrf secref")
	}

	cancel()
	hub.wg.Wait()
}
