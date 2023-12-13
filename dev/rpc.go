package dev

import (
	"github.com/vektra/errors"
	"net/http"
	"regexp"
	_ "strconv"
)

type JsonObj map[string]any

type SimpleHandler func(*http.Request) (int, any, error)

var NotImplementedErr = errors.New("Not Yet Implemented")
var NotFoundErr = errors.New("Path does not exist")
var removeSuffixRe = regexp.MustCompile(`-[a-f0-9]{4,}$`)

func (svc *RpcService) ConfigureRoutes() {
	// see: https://github.com/gorilla/pat?tab=readme-ov-file#example
	// JSON struct tagging: https://stackoverflow.com/a/42549826
	mux := svc.mux
	mux.Get("/", svc.wrapHandler(svc.rpcGetServer))
	mux.Patch("/", svc.wrapHandler(svc.rpcEditServer))
	mux.Del("/", svc.wrapHandler(svc.rpcStopPumaDev))

	mux.Get("/apps", svc.wrapHandler(svc.rpcAppsIndex))
	mux.Del("/apps", svc.wrapHandler(svc.rpcPurgeAppPool))

	mux.Get("/apps/:id", svc.wrapHandler(svc.rpcGetApp))
	mux.Patch("/apps/:id", svc.wrapHandler(svc.rpcUpdateApp))
	mux.Del("/apps/:id", svc.wrapHandler(svc.rpcKillApp))
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

func (svc *RpcService) rpcKillApp(r *http.Request) (int, any, error) {
	app := svc.findAppByRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}

//func (svc *RpcService) rpcDeleteServer(r *http.Request) (int, any, error) {
//	params := svc.reqQueryParams(r)
//	action := params.Get("action")
//	switch action {
//	case "purge":
//
//		return svc.purgePool()
//	case "exit":
//		return svc.stopPumaDev()
//	default:
//		return http.StatusNotImplemented, nil, NotImplementedErr
//	}
//}
