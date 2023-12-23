package dev

import (
	_ "fmt"
	"github.com/vektra/errors"
	"log"
	"net/http"
	"regexp"
	_ "strconv"
	"time"
)

type JsonObj map[string]any

type SimpleHandler func(*http.Request) (int, any, error)

var NotImplementedErr = errors.New("Not Yet Implemented")
var NotFoundErr = errors.New("Path does not exist")
var removeSuffixRe = regexp.MustCompile(`-[a-f0-9]{4,}$`)

func (svc *RpcService) ConfigureRoutes() {
	// see: https://github.com/gorilla/pat?tab=readme-ov-file#example
	// JSON struct tagging: https://stackoverflow.com/a/42549826
	//mux.HandleFunc("/specific", specificHandler)
	//mux.PathPrefix("/").Handler(catchAllHandler)

	mux := svc.mux
	mux.HandleFunc("/", svc.wrapHandler(svc.rpcGetServer)).Methods("GET")
	mux.HandleFunc("/", svc.wrapHandler(svc.rpcEditServer)).Methods("PATCH")
	mux.HandleFunc("/", svc.wrapHandler(svc.rpcStopPumaDev)).Methods("DELETE")

	mux.HandleFunc("/apps", svc.wrapHandler(svc.rpcAppsIndex)).Methods("GET")
	mux.HandleFunc("/apps", svc.wrapHandler(svc.rpcUpdateAppPool)).Methods("PATCH")
	mux.HandleFunc("/apps", svc.wrapHandler(svc.rpcPurgeAppPool)).Methods("DELETE")

	mux.HandleFunc("/apps/{id}", svc.wrapHandler(svc.rpcGetApp)).Methods("GET")
	mux.HandleFunc("/apps/{id}", svc.wrapHandler(svc.rpcUpdateApp)).Methods("PATCH")
	mux.HandleFunc("/apps/{id}", svc.wrapHandler(svc.rpcKillApp)).Methods("DELETE")
	mux.HandleFunc("/apps/{id}/console", svc.wrapHandler(svc.rpcStartAppConsole)).Methods("POST")

	mux.HandleFunc("/events", svc.rpcEventsConnectWS)
	mux.PathPrefix("/").Handler(svc.PublicServer)

}

func (svc *RpcService) rpcGetServer(r *http.Request) (int, any, error) {
	return http.StatusOK, svc.PumaDev.ToJson(), nil
}

func (svc *RpcService) rpcEditServer(r *http.Request) (int, any, error) {
	return http.StatusNotImplemented, nil, NotImplementedErr
}

func (svc *RpcService) rpcStopPumaDev(r *http.Request) (int, any, error) {
	err := Stop()
	if err == nil {
		return http.StatusAccepted, nil, nil
	} else {
		return 0, nil, err
	}
}

func (svc *RpcService) rpcAppsIndex(r *http.Request) (int, any, error) {
	return http.StatusOK, svc.Pool.ToJson(), nil
}

type rpcUpdateAppPoolRequest struct {
	IdleTimeout *string `json:"idleTimeout,omitIfEmpty"`
}

func (svc *RpcService) rpcUpdateAppPool(r *http.Request) (int, any, error) {
	pool := svc.Pool
	reqBody := rpcUpdateAppPoolRequest{}
	err := rpcParseJsonRequestBody[rpcUpdateAppPoolRequest](r, &reqBody)
	if err != nil {
		return http.StatusUnprocessableEntity, nil, err
	}
	if reqBody.IdleTimeout != nil {
		timeout, err := time.ParseDuration(*reqBody.IdleTimeout)
		if err != nil {
			return http.StatusUnprocessableEntity, nil, err
		}
		if timeout < time.Minute {
			return http.StatusUnprocessableEntity, nil, errors.New("idleTimeout must be at least 1 minute")
		}
		pool.lock.Lock()
		pool.IdleTime = timeout
		pool.lock.Unlock()
	}
	return http.StatusOK, nil, nil
}

func (svc *RpcService) rpcPurgeAppPool(r *http.Request) (int, any, error) {
	svc.Pool.Purge()
	return http.StatusAccepted, nil, nil
}

func (svc *RpcService) rpcGetApp(r *http.Request) (int, any, error) {
	app := svc.findAppByRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	jsonApp := app.ToJson(true)

	return http.StatusOK, jsonApp, nil
}

func (svc *RpcService) rpcUpdateApp(r *http.Request) (int, any, error) {
	app := svc.findAppByRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}

var spAppConsole *RpcConsoleProg

func (svc *RpcService) rpcStartAppConsole(r *http.Request) (int, any, error) {
	if spAppConsole != nil {
		return http.StatusLocked, nil, errors.New("Already started")
	}
	app := svc.findAppByRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	var err error
	opts := RpcConsoleProgOpts{}
	*opts.Key = "rails console"
	opts.Argv = []string{"bundle", "exec", "rails", "console"}
	*opts.Interactive = true
	*opts.AllocPty = true

	spAppConsole, err = app.InitConsoleApp(opts)
	if err != nil {
		if spAppConsole != nil {
			_ = spAppConsole.cleanup()
		}
		spAppConsole = nil
		return http.StatusInternalServerError, nil, err
	}
	return http.StatusCreated, nil, nil
}

func (svc *RpcService) rpcKillApp(r *http.Request) (int, any, error) {
	app := svc.findAppByRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}

func (svc *RpcService) rpcDeleteServer(r *http.Request) (int, any, error) {
	params := svc.reqQueryParams(r)
	action := params.Get("action")
	switch action {
	case "purge":
		svc.Pool.Purge()
		svc.wsChannel.UnregisterAll()
		return http.StatusOK, nil, nil
	case "exit":
		svc.wsChannel.Stop()
		err := Stop()
		if err != nil {
			log.Fatalf("Error in RPC exit command: %v", err)
		}
		// TODO: find out how to defer this until after the client gets their response
		return http.StatusOK, nil, nil
	default:
		return http.StatusNotImplemented, nil, NotImplementedErr
	}
}
