package dev

import (
	"encoding/json"
	"github.com/carlmjohnson/truthy"
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
	params := svc.reqQueryParams(r)
	id := svc.PumaDev.removeTLD(params.Get(":id"))
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

func (svc *RpcService) parseJsonReqBody(r *http.Request) (JsonObj, error) {
	bodyReader, err := r.GetBody()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}

	var result map[string]any
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
