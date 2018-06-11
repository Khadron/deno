// Copyright 2018 Ryan Dahl <ry@tinyclouds.org>
// All rights reserved. MIT License.
package deno

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	"github.com/golang/protobuf/proto"
)

const (
	httpChan     = "http"
	serverHeader = "Deno"
)

var (
	httpServers = make(map[int32]*http.Server)
)

func InitHTTP() {
	Sub(httpChan, func(buf []byte) []byte {
		msg := &Msg{}
		check(proto.Unmarshal(buf, msg))
		switch msg.Command {
		case Msg_HTTP_LISTEN:
			httpListen(msg.HttpListenServerId, msg.HttpListenPort)
		case Msg_HTTP_CLOSE:
			httpClose(msg.HttpCloseServerId)
		default:
			panic("[http] Unexpected message " + string(buf))
		}
		return buf
	})
}

var nextReqID int32

func createReqID() (int32, string) {
	id := atomic.AddInt32(&nextReqID, 1)
	return id, fmt.Sprintf("%s/%d", httpChan, id)
}

func buildHTTPHandler(serverID int32) func(w http.ResponseWriter,
	r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// Increment and get an ID for this request.
		id, channelName := createReqID()

		// Used to signal end:
		done := make(chan bool)

		// Subscribe to this channel and handle stuff:
		Sub(channelName, func(buf []byte) []byte {
			msg := &Msg{}
			proto.Unmarshal(buf, msg)
			switch msg.Command {
			case Msg_HTTP_RES_WRITE:
				w.Write(msg.HttpResWriteBody)
			case Msg_HTTP_RES_HEADER:
				w.WriteHeader(int(msg.HttpResHeaderCode))
			case Msg_HTTP_RES_END:
				done <- true
			}
			return buf
		})

		// Prepare and publish request message:
		// TODO stream body.
		var body []byte
		if r.Body != nil {
			body, _ = ioutil.ReadAll(r.Body)
		}
		msg := &Msg{
			Command:       Msg_HTTP_REQ,
			HttpReqBody:   body,
			HttpReqId:     id,
			HttpReqMethod: r.Method,
			HttpReqPath:   r.URL.Path,
		}
		go PubMsg(httpChan, msg)

		w.Header().Set("Server", serverHeader)

		// Block and wait for done signal:
		<-done
	}
}

func httpListen(serverID int32, port int32) {
	if !Perms.Net {
		panic("Network access denied")
	}
	s := &http.Server{}
	httpServers[serverID] = s
	listenAddr := fmt.Sprintf(":%d", port)
	handler := buildHTTPHandler(serverID)
	s.Addr = listenAddr
	s.Handler = http.HandlerFunc(handler)
	wg.Add(1)
	go func() {
		s.ListenAndServe()
		wg.Done()
	}()
}

func httpClose(serverID int32) {
	s, ok := httpServers[serverID]
	if !ok {
		panic("[http] Server not found")
	}
	s.Close()
}
