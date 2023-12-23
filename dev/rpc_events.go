package dev

import (
	"encoding/json"
	WebSocketChat "github.com/puma/puma-dev/dev/websockets"
	"log"
	"net/http"
	"strings"
)

func (svc *RpcService) rpcEventsConnectWS(w http.ResponseWriter, r *http.Request) {
	cb := func(c *WebSocketChat.Client, msg []byte) error {
		// TODO: handle subscribe/unsubscribe/etc commands
		log.Printf("recv: %s", msg)
		return c.Send([]byte("Incoming websocket messages unsupported"), "error")
	}
	_, err := svc.wsChannel.Serve(w, r, WebSocketChat.HubServeOpts{
		OnMessage: cb,
		// TODO: support setting this in the request
		Subscriptions: []string{"*"},
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (svc *RpcService) handleEvent(event string, tags ...string) {
	var obj map[string]any
	err := json.Unmarshal([]byte(event), &obj)
	if err != nil {
		log.Printf("Failed to unmarshall event: %s", event)
		return
	}
	if obj["app"] != nil {
		tags = append(tags, obj["app"].(string))
	} else {
		tags = append(tags, "general")
	}
	if obj["tags"] != nil {
		tags = append(tags, obj["tags"].([]string)...)
	}
	svc.wsChannel.Broadcast([]byte(event), tags...)
}

func (svc *RpcService) handleLog(a *App, line string) {
	line = strings.TrimSpace(line)
	a.eventAdd("console_log", "message", line)
}
