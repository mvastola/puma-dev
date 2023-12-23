package dev

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	WebSocketChat "github.com/puma/puma-dev/dev/websockets"
	"github.com/puma/puma-dev/homedir"
	"log"
	"net"
	"net/http"
	"os"
)

const RpcSocketPath = "~/.puma-dev.mgmt.sock"
const RpcPublicDir = "~/src/puma-dev/public"
const RpcTcpPort = 8080

var StatusLabels = [...]string{"booting", "running", "dead"}

var rpcService RpcService

type RpcListenAddr struct {
	network string
	addr    string
}

type RpcService struct {
	Pid          int
	TcpPort      int16
	SocketPath   string
	PublicDir    string
	PublicServer http.Handler
	Pool         *AppPool
	PumaDev      *HTTPServer

	mux          *mux.Router
	wsChannel    *WebSocketChat.Hub
	wsAppChannel map[string]WebSocketChat.Hub
	listeners    []net.Listener
	ctrlServer   *http.Server
	initialized  bool
}

func (svc *RpcService) init(h *HTTPServer) {
	svc.initialized = false
	svc.PumaDev = h
	svc.Pid = os.Getpid()
	svc.Pool = h.Pool
	svc.mux = mux.NewRouter()
	svc.ctrlServer = &http.Server{
		Handler: svc,
	}
	svc.wsChannel = WebSocketChat.NewHub()
	svc.SocketPath = homedir.MustExpand(RpcSocketPath)
	svc.PublicDir = homedir.MustExpand(RpcPublicDir)
	svc.PublicServer = http.FileServer(http.Dir(svc.PublicDir))
	svc.TcpPort = RpcTcpPort
	svc.initialized = true
}

func (svc *RpcService) listen() {
	addListener := func(network string, addr string) {
		listener, err := net.Listen(network, addr)
		if err != nil {
			log.Fatalf("Error opening socket %s for RPC service: %s", svc.SocketPath, err)
		}
		svc.listeners = append(svc.listeners, listener)
	}
	addListener("unix", homedir.MustExpand(svc.SocketPath))
	addListener("tcp4", fmt.Sprintf("%s:%d", "localhost", svc.TcpPort))
}

func (svc *RpcService) serveListener(listener *net.Listener) {
	err := svc.ctrlServer.Serve(*listener)
	if err != nil {
		log.Fatalf("Error starting RPC service: %s", err)
	}
}

func (svc *RpcService) start() {
	_ = os.Remove(svc.SocketPath)
	svc.listen()

	for _, listener := range svc.listeners {
		go svc.serveListener(&listener)
	}
}

func (svc *RpcService) wrapHandler(handler SimpleHandler) http.HandlerFunc {
	wrapper := func(w http.ResponseWriter, r *http.Request) {

		status, response, err := handler(r)
		if err != nil && status <= 0 {
			status = http.StatusInternalServerError
		}
		if status > 0 {
			w.WriteHeader(status)
		}
		if err != nil && NotFoundErr.Error() == err.Error() {
			log.Printf("RPC path not found: %s", r.URL.String())
			http.NotFound(w, r)
			return
		} else if err != nil {
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if response == nil {
			return
		}
		result, err := json.MarshalIndent(response, "", "  ")
		if err == nil {
			if status <= 0 {
				w.WriteHeader(status)
			}
			_, _ = w.Write(result)
		} else {
			log.Printf("Warning during response body encoding: %s", err)
		}
	}

	return wrapper
}

func (h *HTTPServer) StartRPC() *RpcService {
	rpcService.init(h)
	rpcService.ConfigureRoutes()
	go func() { rpcService.start() }()
	go func() { rpcService.wsChannel.Run() }()
	return &rpcService
}

func (svc *RpcService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: verify permissions first(?)
	svc.mux.ServeHTTP(w, r)
}
