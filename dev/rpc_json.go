package dev

import (
	"github.com/carlmjohnson/truthy"
	"strings"
)

func (pd *HTTPServer) ToJson() JsonObj {
	pool := pd.Pool
	obj := JsonObj{}
	obj["address"] = pd.Address
	obj["tlsAddress"] = pd.TLSAddress
	obj["debug"] = pd.Debug
	obj["ignoredStaticPaths"] = pd.IgnoredStaticPaths
	obj["domains"] = pd.Domains
	obj["idleTime"] = pool.IdleTime
	obj["rootDirectory"] = pool.Dir
	return obj

}
func (pool *AppPool) ToJson() []JsonObj {
	apps := []JsonObj{}
	for _, app := range pool.apps {
		apps = append(apps, app.ToJson(false))
	}
	return apps
}

func (app *App) ToJson(fullData bool) JsonObj {
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
