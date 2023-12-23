// Adapted from https://github.com/gorilla/websocket/blob/dcea2f088ce10b1b0722c4eb995a4e145b5e9047/examples/chat
// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package WebSocketChat

import "bytes"

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Broadcasts to the clients.
	send chan []byte

	// register requests from the clients.
	register chan *Client

	// unregister requests from clients.
	unregister chan *Client

	stop chan bool
}

func NewHub() *Hub {
	return &Hub{
		send:       make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		stop:       make(chan bool),
	}
}

func (h *Hub) Stop() {
	h.stop <- true
}

func (h *Hub) shutdown() {
	close(h.register)
	close(h.unregister)
	close(h.send)
	close(h.stop)
	for client := range h.clients {
		client.close()
	}
}

func (h *Hub) eachClient(fn func(c *Client)) {
	for client := range h.clients {
		fn(client)
	}
}

func (h *Hub) broadcast(message []byte) {
	h.eachClient(func(c *Client) {
		select {
		case c.send <- message:
		default:
			c.close()
		}
	})
}

func (h *Hub) UnregisterAll() {
	for client := range h.clients {
		h.unregister <- client
	}
}

func (h *Hub) Run() {
	var stop = false
	for !stop {
		select {
		case <-h.stop:
			stop = true
		case client, ok := <-h.register:
			if ok {
				h.clients[client] = true
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				client.close()
			}
		case message := <-h.send:
			h.broadcast(message)
		}
	}
	h.shutdown()
}

func (h *Hub) Broadcast(message []byte) {
	message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
	h.send <- message
}
