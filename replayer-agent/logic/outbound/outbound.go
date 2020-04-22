package outbound

import (
	"context"
	"net"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/didi/sharingan/replayer-agent/common/handlers/tlog"
	"github.com/didi/sharingan/replayer-agent/logic/match"
	"github.com/didi/sharingan/replayer-agent/logic/replayed"
	"github.com/didi/sharingan/replayer-agent/model/replaying"
	"github.com/didi/sharingan/replayer-agent/model/station"
)

const (
	fakeIndexNotMatched = -1
	fakeIndexSimulated  = -2
)

func Start(addr *net.TCPAddr) {
	defer func() {
		if err := recover(); err != nil {
			tlog.Handler.Errorf(context.Background(), tlog.DLTagUndefined, "panic in %s goroutine||errmsg=%s||stack info=%s", "StartOutboundServer", err, strconv.Quote(string(debug.Stack())))
		}
	}()
	listener, err := net.Listen("tcp", addr.String())
	if err != nil {
		tlog.Handler.Errorf(context.Background(), tlog.DLTagUndefined, "errmsg=listen outbound failed||err=%s", err)
		return
	}
	tlog.Handler.Infof(context.Background(), tlog.DLTagUndefined, "outbound server started||outboundAddr=%s", addr)
	for {
		conn, err := listener.(*net.TCPListener).AcceptTCP()
		if err != nil {
			tlog.Handler.Errorf(context.Background(), tlog.DLTagUndefined, "errmsg=accept outbound failed||err=%s", err)
			return
		}
		go handleOutbound(addr, conn)
	}
}

type Handler struct {
	ctx              context.Context    // 串联日志
	matcher          *match.Matcher     // 匹配引擎
	replayingSession *replaying.Session // 待匹配session
	replayedSession  *replayed.Session  // 记录匹配详细信息
}

type Server struct {
	sync.Mutex
	Handlers map[string]*Handler
}

var OutboundServer Server

func loadHandler(ctx context.Context, traceID string) *Handler {
	if traceID == "" {
		return nil
	}

	OutboundServer.Lock()
	defer OutboundServer.Unlock()

	handler, ok := OutboundServer.Handlers[traceID]
	if !ok {
		return nil
	}
	return handler
}

func StoreHandler(ctx context.Context, traceID string) {
	if traceID == "" {
	}

	OutboundServer.Lock()
	defer OutboundServer.Unlock()

	handler := &Handler{}
	handler.ctx = ctx
	handler.matcher = match.New()
	handler.replayingSession, handler.replayedSession = station.Load(traceID)
	OutboundServer.Handlers[traceID] = handler
}

func RemoveHandler(ctx context.Context, traceID string) {
	OutboundServer.Lock()
	defer OutboundServer.Unlock()
	delete(OutboundServer.Handlers, traceID)
}

func handleOutbound(serverAddr *net.TCPAddr, conn *net.TCPConn) {
	ctx := context.Background()
	defer func() {
		if err := recover(); err != nil {
			tlog.Handler.Errorf(ctx, tlog.DLTagUndefined, "panic in %s goroutine||errmsg=%s||stack info=%s", "HandleOutbound", err, strconv.Quote(string(debug.Stack())))
		}
	}()
	defer conn.Close()

	tcpAddr := conn.RemoteAddr().(*net.TCPAddr)
	tlog.Handler.Debugf(ctx, tlog.DebugTag, "new outbound||addr=%s||begin", tcpAddr.String())

	cs := &ConnState{
		LastMatchedIndex: -1,
		conn:             conn,
		tcpAddr:          tcpAddr,
		proxyer:          NewProxyer(conn),
	}

	for i := 0; ; i++ {
		cont := cs.ProcessRequest(ctx, i)
		if !cont {
			break
		}
	}
	cs.proxyer.Close()
	tlog.Handler.Debugf(ctx, tlog.DebugTag, "new outbound||addr=%s||end", tcpAddr.String())
}
