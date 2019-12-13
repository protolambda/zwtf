package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"
	"zwtf/events"
	"zwtf/fetch"
	"zwtf/memory"

	"github.com/gorilla/websocket"
)

var eventsWsAddr = flag.String("events-ws-addr", "localhost:5053", "Event websocket address")
var restHttpAddr = flag.String("rest-http-addr", "localhost:5052", "REST API http address")

func main() {
	flag.Parse()
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	fetcher := fetch.NewBeaconAPIFetcher(*restHttpAddr)

	memMng := memory.NewMemoryManager(fetcher.GetStateByBlockRoot)

	u := url.URL{Scheme: "ws", Host: *eventsWsAddr, Path: ""}
	log.Printf("connecting to events websocket: %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("event ws read error:", err)
				continue
			}
			log.Printf("recv: %s", message)
			var ev events.Event
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Println("event decode error:", err)
				continue
			}
			memMng.OnEvent(&ev)
		}
	}()

	// TODO: open http api to serve full memory
	// TODO: open websocket to serve memory diffs

	pruneTicker := time.NewTicker(time.Second * 10)
	defer pruneTicker.Stop()

	for {
		select {
		case <-done:
			return
		case <-pruneTicker.C:
			log.Println("pruning old memory")
			memMng.PruneBlocks()
			memMng.PruneVotes()
		case <-interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
