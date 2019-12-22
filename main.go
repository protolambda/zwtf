package main

import (
	"encoding/json"
	"flag"
	"github.com/protolambda/zwtf/events"
	"github.com/protolambda/zwtf/fetch"
	"github.com/protolambda/zwtf/memory"
	"github.com/protolambda/zwtf/server/hub"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)


var serverWs = flag.String("serve-addr", ":4000", "serve address")

var eventsWs = flag.String("events-ws", "ws://localhost:5053", "Event websocket address")
var restHttp = flag.String("rest-http", "http://localhost:5052", "REST API http address")

func main() {
	flag.Parse()
	log.SetFlags(0)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	log.Printf("fetching api data from: %s", *restHttp)
	fetcher := fetch.NewBeaconAPIFetcher(*restHttp)

	memMng := memory.NewMemoryManager(memory.StateGetter(fetcher.GetStateByBlockRoot))

	log.Printf("connecting to events websocket: %s", *eventsWs)

	c, _, err := websocket.DefaultDialer.Dial(*eventsWs, nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	doneEvents := make(chan struct{})

	eventsCh := make(chan events.Event)
	go func() {
		defer close(doneEvents)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("event ws read error:", err)
				return // TODO try reconnect?
			}
			var ev events.Event
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Println("event decode error:", err)
				continue
			}
			eventsCh <- ev
		}
	}()

	// This will maintain all client connections, to broadcast diffs to
	clHub := hub.NewHub()

	// open a little server to provide the websocket endpoint in a browser-friendly way.
	go func() {
		httpServer := http.NewServeMux()
		httpServer.HandleFunc("/ws", clHub.ServeWs)
		// accept connections
		if err := http.ListenAndServe(*serverWs, httpServer); err != nil {
			log.Fatal("client hub server err: ", err)
		}
	}()

	diffTicker := time.NewTicker(time.Second * 3)
	defer diffTicker.Stop()

	pruneTicker := time.NewTicker(time.Second * 10)
	defer pruneTicker.Stop()

	for {
		select {
		case <-doneEvents:
			return
		case ev := <-eventsCh:
			log.Println("processing event: ", ev.Event)
			memMng.OnEvent(&ev)
			log.Println("finished processing event: ", ev.Event)
		case <-diffTicker.C:
			log.Println("diffing state")
			diff := memMng.BuildDiff()
			//log.Println(diff.Display())
			out, err := json.Marshal(&diff)
			if err != nil {
				log.Println(err)
			}
			log.Println(string(out))
			clHub.Broadcast(out)
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
			case <-doneEvents:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
