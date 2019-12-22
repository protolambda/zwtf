package hub

import (
	"github.com/gorilla/websocket"
	"github.com/protolambda/zwtf/server/client"
	"log"
	"net/http"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*client.Client]bool

	// Register requests from the clients.
	register chan *client.Client

	// Unregister requests from clients.
	unregister chan *client.Client
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *client.Client),
		unregister: make(chan *client.Client),
		clients:    make(map[*client.Client]bool),
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Start serving a new client
func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	upgrader.CheckOrigin = func(r *http.Request) bool {
		// allow any origin to connect.
		return true
	}

	if len(h.clients) > 100 {
		log.Println("too many clients!") // TODO temporary safety measure; decide on clients limit later.
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	log.Println("onboarding new client")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	var c *client.Client
	c = client.NewClient(conn, func() {
		h.unregister <- c
	})

	// register it
	h.register <- c

	// start processing routines for the client
	go c.WritePump()
	go c.ReadPump()
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				c.Close()
			}
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	for cl, _ := range h.clients {
		cl.Send(msg)
	}
	return
}
