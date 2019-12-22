package client

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512

	// Maximum amounts of messages to buffer to a client before disconnecting them
	buffedMsgCount = 200
)

var newline = []byte{'\n'}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	unregister func()

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	closed    bool
	closeLock sync.Mutex
}

func NewClient(conn *websocket.Conn, unregister func()) *Client {
	sendCh := make(chan []byte, buffedMsgCount)
	return &Client{
		unregister: unregister,
		conn:       conn,
		send:       sendCh,
		closed:     false,
		closeLock:  sync.Mutex{},
	}
}

func (c *Client) Close() {
	c.closeLock.Lock()
	c.closed = true
	close(c.send)
	c.closeLock.Unlock()
}

// ReadPump pumps messages from the websocket connection to the client message handler.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.unregister()
		if err := c.conn.Close(); err != nil {
			log.Printf("Client %v unregistered with an error: %v", c, err)
		}
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
		c.onMsg(message)
	}
	log.Println("quiting client")
}

// WritePump pumps messages from the client send channel to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if err := c.conn.Close(); err != nil {
			log.Printf("Stopped connection with client %v, but with an error: %v", c, err)
		}
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return
			}
			//log.Printf("%x", message)
			if _, err := w.Write(message); err != nil {
				log.Printf("Error when sending msg to client: err: %v", err)
			}

			// Add queued chat messages to the current websocket message.
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

func (c *Client) Send(msg []byte) {
	c.closeLock.Lock()
	if !c.closed {
		c.send <- msg
	}
	c.closeLock.Unlock()
}

func (c *Client) onMsg(msg []byte) {
	log.Printf("received msg from client: %v: %s", c, string(msg))
}
