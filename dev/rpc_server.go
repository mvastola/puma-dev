package dev

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/puma/puma-dev/homedir"
	"log"
	"net"
	"net/http"
	"os"
)

const RpcSocketPath = "~/.puma-dev.mgmt.sock"
const RpcTcpPort = 8080

var StatusLabels = [...]string{"booting", "running", "dead"}

var rpcService RpcService

type RpcListenAddr struct {
	network string
	addr    string
}

type RpcService struct {
	TcpPort    int16
	SocketPath string
	Pool       *AppPool
	PumaDev    *HTTPServer
	mux        *mux.Router

	listeners   []net.Listener
	ctrlServer  *http.Server
	initialized bool
}

func (svc *RpcService) init(h *HTTPServer) {
	svc.initialized = false
	svc.PumaDev = h
	svc.Pool = h.Pool
	svc.mux = mux.NewRouter()
	svc.ctrlServer = &http.Server{
		Handler: svc,
	}
	svc.SocketPath = homedir.MustExpand(RpcSocketPath)
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

	var listener *net.Listener = nil
	for listener = range svc.listeners {
		go svc.serveListener(listener)
	}
}

func (svc *RpcService) wrapHandler(handler SimpleHandler) http.HandlerFunc {
	wrapper := func(w http.ResponseWriter, r *http.Request) {

		status, response, err := handler(r)
		if status > 0 {
			w.WriteHeader(status)
		}
		if err != nil && NotFoundErr.Error() == err.Error() {
			http.NotFound(w, r)
			return
		} else if err != nil {
			w.WriteHeader(500)
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
	return &rpcService
}

func (svc *RpcService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: verify permissions first
	svc.mux.ServeHTTP(w, r)
}
