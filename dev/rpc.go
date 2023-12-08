package dev

import (
	"github.com/DavidHuie/quartz/go/quartz"
	"log"

	_ "encoding/json"
	_ "net/http"
	_ "strconv"

	"reflect"
)

var (
	rpcInterface = RpcInterface{}
)

type RpcInterface struct {
	Pool *AppPool
	Http *HTTPServer
}

func SetupQuartz(server *HTTPServer) {
	rpcInterface.Http = server
	rpcInterface.Pool = server.Pool

	// Reflect all fields in struct to quartz
	fields := reflect.VisibleFields(reflect.TypeOf(rpcInterface))
	reflection := reflect.ValueOf(rpcInterface)
	for _, field := range fields {
		if field.Anonymous {
			continue
		}
		name := field.Name
		value := reflection.FieldByName(name).Interface()
		err := quartz.RegisterName(name, value)
		if err != nil {
			log.Printf("! Failed to register RPC struct %s with Quartz: %s", name, err)
		} else {
			log.Printf("Register RPC struct %s with Quartz", name)
		}
	}
	quartz.Start()
}
