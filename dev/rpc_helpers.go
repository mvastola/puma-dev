package dev

import (
	"encoding/json"
	"github.com/carlmjohnson/truthy"
	"github.com/gorilla/mux"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
)

func (svc *RpcService) reqQueryParams(r *http.Request) url.Values {
	return r.URL.Query()
}

func (svc *RpcService) findAppByRequest(r *http.Request) *App {
	pathParams := mux.Vars(r)
	params := svc.reqQueryParams(r)
	id := svc.PumaDev.removeTLD(pathParams[":id"])
	tryCreateIfMissing := !truthy.ValueAny(params.Get("noCreate"))
	return svc.findAppByKey(id, tryCreateIfMissing)
}

func (svc *RpcService) findAppByKey(id string, tryCreateIfMissing bool) *App {
	pool := svc.Pool
	apps := pool.apps
	if apps[id] != nil {
		return apps[id]
	}

	if tryCreateIfMissing {
		app, err := pool.lookupApp(id)
		if err != nil {
			log.Printf("Error looking up app named '%s': %s", id, err)
		}
		return app
	}

	simpleId := removeSuffixRe.ReplaceAllString(id, "")
	expectedPath := path.Join(pool.Dir, simpleId)
	expectedPathReal, err := filepath.EvalSymlinks(expectedPath)

	if err != nil || !truthy.ValueAny(expectedPathReal) {
		return nil
	}

	for _, app := range apps {
		appDirReal, err := filepath.EvalSymlinks(app.dir)
		if err != nil && expectedPathReal == appDirReal {
			return app
		}
	}

	// last try... lets try to find any app with simpleId
	// as the key, after removing the hex suffix
	for appId, app := range apps {
		simpleAppId := removeSuffixRe.ReplaceAllString(appId, "")
		if simpleId == simpleAppId {
			return app
		}
	}
	return nil
}

// default: JsonObj
func rpcParseJsonRequestBody[T interface{}](r *http.Request, target *T) error {
	bodyReader, err := r.GetBody()
	if err != nil {
		return err
	}
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}

	err = json.Unmarshal(body, target)
	if err != nil {
		return err
	}
	return nil
}
