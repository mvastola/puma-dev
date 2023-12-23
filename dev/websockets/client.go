// Adapted from https://github.com/gorilla/websocket/blob/dcea2f088ce10b1b0722c4eb995a4e145b5e9047/examples/chat
// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package WebSocketChat

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cornelk/hashmap"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a payload to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong payload from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum payload size allowed from peer.
	maxMessageSize = 2048
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  maxMessageSize * 2,
	WriteBufferSize: maxMessageSize * 2,
}

type MessageCallback func(c *Client, msg []byte) error

type HubServeOpts struct {
	OnMessage     MessageCallback
	Subscriptions []string
}

// Client is a middleman between the websocket connection and the Hub.
type Client struct {
	Hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// which apps' events the client is subscribed to
	subscriptions *hashmap.Map[string, bool]
	// Buffered channel of inbound messages.
	recv chan []byte
	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the Hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
//
//goland:noinspection GoUnhandledErrorResult
func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		// TODO: Reply with payload rejecting any commands
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		if c.recv != nil {
			c.recv <- message
		} else {
			// TODO: should prob reply with error payload
			// for now though, just discard and log a warning
			log.Printf("Recieved unexpected payload via websockets: %s", message)
		}
	}
}

// writePump pumps messages from the Hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
//
//goland:noinspection GoUnhandledErrorResult
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The Hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket payload.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Send(data []byte, tag string) error {
	select {
	case c.send <- data:
		return nil
	default:
		return errors.New(fmt.Sprintf("Failed to send %s to client", data))
	}
}

func (c *Client) listen(cb MessageCallback) {
	handleMessage := func(message []byte, ok bool) error {
		if ok {
			return cb(c, message)
		} else { // quit if recv channel closed
			return errors.New("connection closed")
		}
	}

	var err error = nil
	for err == nil {
		select {
		case data, ok := <-c.recv:
			err = handleMessage(data, ok)
		}
	}

}

func (c *Client) close() {
	delete(c.Hub.clients, c)
	if c.recv != nil {
		close(c.recv)
	}
	close(c.send)
}

func (c *Client) Subscribe(names ...string) {
	for _, name := range names {
		c.subscriptions.Set(name, true)
	}
}

func (c *Client) Unsubscribe(names ...string) {
	for _, name := range names {
		c.subscriptions.Del(name)
	}
}

func (c *Client) IsSubscribed(names ...string) bool {
	wildcard, _ := c.subscriptions.GetOrInsert("*", false)
	if wildcard {
		return true
	}
	for _, name := range names {
		value, _ := c.subscriptions.GetOrInsert(name, false)
		if value {
			return true
		}
	}
	return false
}

// Serve handles websocket requests from the peer.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, opts HubServeOpts) (*Client, error) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	client := &Client{
		Hub:           h,
		conn:          conn,
		send:          make(chan []byte, maxMessageSize*4),
		recv:          nil,
		subscriptions: hashmap.New[string, bool](),
	}

	var argSubs []string = []string{"errors", "broadcast"}
	if opts.Subscriptions != nil {
		argSubs = append(argSubs, opts.Subscriptions...)
	}
	client.Subscribe(argSubs...)

	if opts.OnMessage != nil {
		client.recv = make(chan []byte, maxMessageSize*4)
	}
	client.Hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
	if opts.OnMessage != nil {
		go client.listen(opts.OnMessage)
	}
	return client, nil
}
