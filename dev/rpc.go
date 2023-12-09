package dev

import (
	"context"
	"encoding/json"
	"github.com/bmizerany/pat"
	"github.com/carlmjohnson/truthy"
	"github.com/puma/puma-dev/homedir"
	"github.com/vektra/errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	_ "strconv"
	"strings"
	"unsafe"
)

const RPC_SOCKET_PATH = "~/.puma-dev.mgmt.sock"

type SimpleHandler func(*http.Request) (int, any, error)

var NotImplementedErr = errors.New("Not Yet Implemented")
var NotFoundErr = errors.New("Path does not exist")

var StatusLabels = [...]string{"booting", "running", "dead"}

//type PumaDevJson struct {
//	Address            string       `json:"address"`
//	TLSAddress         string       `json:"tlsAddress"`
//	Pool               *AppPoolJson `json:"pool"`
//	Debug              bool         `json:"debug"`
//	Events             *Events      `json:"-"`
//	IgnoredStaticPaths []string     `json:"ignoredStaticPaths"`
//	Domains            []string     `json:"domains"`
//
//	mux           *pat.PatternServeMux
//	unixTransport *http.Transport
//	unixProxy     *httputil.ReverseProxy
//	tcpTransport  *http.Transport
//	tcpProxy      *httputil.ReverseProxy
//}
//
//type AppPoolJson struct {
//	Dir      string        `json:"directory"`
//	IdleTime time.Duration `json:"idleTime"`
//	Debug    bool          `json:"debug"`
//	Events   *Events       `json:"-"`
//
//	AppClosed func(*App) `json:"-"`
//
//	lock sync.Mutex
//	apps map[string]*App
//}
//
//type AppJson struct {
//	Name    string
//	Scheme  string
//	Host    string
//	Port    int
//	Command *exec.Cmd
//	Public  bool
//	Events  *Events
//
//	lines       linebuffer.LineBuffer
//	lastLogLine string
//
//	address string
//	dir     string
//
//	t tomb.Tomb
//
//	stdout  io.Reader
//	pool    *AppPool
//	lastUse time.Time
//
//	lock sync.Mutex
//
//	booting bool
//
//	readyChan chan struct{}
//}

type RpcService struct {
	SocketPath string
	mux        *pat.PatternServeMux
	listener   *net.Listener
	ctrlServer *http.Server
	iface      *RpcInterface
}

type RpcInterface struct {
	Pool    *AppPool
	PumaDev *HTTPServer
}

func (svc *RpcService) Init() {
	svc.mux = pat.New()
	svc.ctrlServer = &http.Server{
		Handler: svc,
	}
	svc.SocketPath = homedir.MustExpand(RPC_SOCKET_PATH)
}

// From https://stackoverflow.com/a/60598827
func GetUnexportedField(field reflect.Value) interface{} {
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
}

//func SetUnexportedField(field reflect.Value, value interface{}) {
//	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
//		Elem().
//		Set(reflect.ValueOf(value))
//}

//GetUnexportedField(reflect.ValueOf(&Foo{}).Elem().FieldByName("unexportedField"))
// End from https://stackoverflow.com/a/60598827

//func (svc *RpcService) onlyFields() {
//	fields := reflect.VisibleFields(reflect.TypeOf(svc))
//	reflection := reflect.ValueOf(svc)
//	for _, field := range fields {
//		if field.Anonymous {
//			continue
//		}
//		name := field.Name
//		fieldValue := reflection.FieldByName(name) //.Interface()
//		value := GetUnexportedField(fieldValue)
//
//		fmt.Printf("%s: %s", name, value)
//		//err := quartz.RegisterName(name, value)
//		//if err != nil {
//		//	log.Printf("! Failed to register RPC struct %s with Quartz: %s", name, err)
//		//} else {
//		//	log.Printf("Register RPC struct %s with Quartz", name)
//		//}
//	}
//}

func (svc *RpcService) connectionContext(ctx context.Context, c net.Conn) context.Context {
	// TODO: some sort of access control? (unix permissions on the socket should address that though)
	return ctx
}

func (svc *RpcService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: verify permissions first
	svc.mux.ServeHTTP(w, r)
}

func (svc *RpcService) Start() {
	_ = os.Remove(svc.SocketPath)
	listener, err := net.Listen("unix", svc.SocketPath)
	if err != nil {
		log.Fatalf("Error opening socket %s for RPC service: %s", svc.SocketPath, err)
	}
	svc.listener = &listener
	err = svc.ctrlServer.Serve(listener)
	if err != nil {
		log.Fatalf("Error starting RPC service: %s", err)
	}
}

func (h *HTTPServer) wrapHandler(handler SimpleHandler) http.HandlerFunc {
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

func (h *HTTPServer) ServeRPC() *RpcService {
	svc := &RpcService{}
	svc.Init()

	svc.iface = &RpcInterface{
		PumaDev: h,
		Pool:    h.Pool,
	}
	// see: https://github.com/gorilla/pat?tab=readme-ov-file#example
	// JSON struct tagging: https://stackoverflow.com/a/42549826
	svc.mux.Get("/", h.wrapHandler(svc.getServer))
	svc.mux.Patch("/", h.wrapHandler(svc.modifyServer))
	svc.mux.Del("/", h.wrapHandler(svc.stopServer))
	svc.mux.Get("/apps/:id", h.wrapHandler(svc.getApp))
	svc.mux.Patch("/apps/:id", h.wrapHandler(svc.modifyApp))
	svc.mux.Del("/apps/:id", h.wrapHandler(svc.deleteApp))

	go func() {
		svc.Start()
	}()
	return svc
}

func (svc *RpcService) getRequestBody(r *http.Request) (any, error) {
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

type JsonObj map[string]any

func (svc *RpcService) getServer(r *http.Request) (int, any, error) {
	pd := svc.iface.PumaDev
	pool := pd.Pool

	obj := JsonObj{}
	obj["address"] = pd.Address
	obj["tlsAddress"] = pd.TLSAddress
	obj["debug"] = pd.Debug
	obj["ignoredStaticPaths"] = pd.IgnoredStaticPaths
	obj["domains"] = pd.Domains
	obj["idleTime"] = pool.IdleTime
	obj["rootDirectory"] = pool.Dir
	apps := []JsonObj{}
	for _, app := range pool.apps {
		apps = append(apps, svc.getAppJson(app, false))
	}
	obj["apps"] = apps
	return http.StatusOK, obj, nil
}

func (svc *RpcService) modifyServer(r *http.Request) (int, any, error) {
	return http.StatusNotImplemented, nil, NotImplementedErr
}

func (svc *RpcService) stopServer(r *http.Request) (int, any, error) {
	err := Stop()
	if err == nil {
		return http.StatusAccepted, nil, nil
	} else {
		return 0, nil, err
	}
}

func (svc *RpcService) newApp(r *http.Request) (int, any, error) {
	_, err := svc.getRequestBody(r)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}

var removeSuffixRe = regexp.MustCompile(`-[a-f0-9]{4,}$`)

func (svc *RpcService) getAppForRequest(r *http.Request) *App {
	id := svc.iface.PumaDev.removeTLD(r.URL.Query().Get(":id"))
	tryCreateIfMissing := !truthy.ValueAny(r.URL.Query().Get("noCreate"))
	pool := svc.iface.Pool
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

func (svc *RpcService) getAppJson(app *App, fullData bool) JsonObj {
	jsonApp := JsonObj{}
	jsonApp["id"] = app.Name
	jsonApp["name"] = app.Name
	if len(app.Host) > 0 {
		jsonApp["host"] = app.Host
	}
	jsonApp["scheme"] = app.Scheme
	if app.Port > 0 {
		jsonApp["port"] = app.Port
	}
	if app.Public {
		jsonApp["public"] = app.Public
	}
	if truthy.ValueAny(app.address) {
		jsonApp["address"] = app.address
	}
	jsonApp["directory"] = app.dir

	if !fullData {
		return jsonApp
	}

	jsonApp["booting"] = app.booting
	jsonApp["status"] = StatusLabels[app.Status()]
	if truthy.ValueAny(app.lastLogLine) {
		jsonApp["lastLogLine"] = strings.Trim(app.lastLogLine, "\n\r\t ")
	}
	if app.Command != nil {
		cmd := app.Command
		command := JsonObj{}
		command["environment"] = cmd.Env
		command["directory"] = cmd.Dir
		command["arguments"] = cmd.Args
		command["path"] = cmd.Path
		if truthy.ValueAny(command["extraFiles"]) {
			command["extraFiles"] = cmd.ExtraFiles
		}
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			command["pid"] = cmd.Process.Pid
		}
		jsonApp["command"] = command
	} else {
		jsonApp["command"] = nil
	}

	if app.Command != nil && app.Command.ProcessState != nil {
		state := app.Command.ProcessState
		result := JsonObj{}
		result["exitCode"] = state.ExitCode()
		result["exited"] = state.Exited()
		result["success"] = state.Success()
		result["systemTime"] = state.SystemTime()
		result["userTime"] = state.UserTime()
		jsonApp["result"] = result
	}
	return jsonApp
}

func (svc *RpcService) getApp(r *http.Request) (int, any, error) {
	app := svc.getAppForRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	jsonApp := getAppJson(app, true)

	return http.StatusOK, jsonApp, nil
}

func (svc *RpcService) modifyApp(r *http.Request) (int, any, error) {
	app := svc.getAppForRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}

func (svc *RpcService) deleteApp(r *http.Request) (int, any, error) {
	app := svc.getAppForRequest(r)
	if app == nil {
		return http.StatusNotFound, nil, NotFoundErr
	}
	return http.StatusNotImplemented, nil, NotImplementedErr
}
