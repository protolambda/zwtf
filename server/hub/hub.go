package hub

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/protolambda/zwtf/server/client"
	"log"
	"net/http"
	"sync"
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

	ctx context.Context
}

func NewHub(ctx context.Context) *Hub {
	return &Hub{
		register:   make(chan *client.Client),
		unregister: make(chan *client.Client),
		clients:    make(map[*client.Client]bool),
		ctx: ctx,
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
		case <-h.ctx.Done():
			h.KickAll()
			return
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

func (h *Hub) KickAll() {
	var wg sync.WaitGroup
	for cl, _ := range h.clients {
		wg.Add(1)
		go func(c *client.Client) {
			c.Close()
		}(cl)
	}
	wg.Done()
}

func (h *Hub) Broadcast(msg []byte) {
	for cl, _ := range h.clients {
		log.Printf("sending msg to client")
		cl.Send(msg)
	}
	return
}
