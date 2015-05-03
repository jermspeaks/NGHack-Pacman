/*  OpenBCI golang server allows users to control, visualize and store data
    collected from the OpenBCI microcontroller.
    Copyright (C) 2015  Kevin Schiesser

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as
    published by the Free Software Foundation, either version 3 of the
    License, or (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/kevinjos/openbci-golang-server/openbci"
)

var addr = flag.String("addr", ":8888", "http service address")
var location = flag.String("loc", "/dev/ttyUSB0", "serial mount point")
var baud = flag.Int("baud", 115200, "serial baud rate")
var readTimeout = time.Millisecond

const (
	channels         = 8
	samplesPerSecond = 250
	readBufferSize   = 1024 * 1024
	RawMsgSize       = 250
)

func main() {
	flag.DurationVar(&readTimeout, "rt", 100*time.Millisecond, "serial readtimeout in milliseconds")
	flag.Parse()

	h := NewHub()

	device, err := openbci.NewDevice(*location, *baud, readTimeout)
	if err != nil {
		log.Fatalf("error opening device: %s\n", err)
	}
	predictionServerConn, err := net.Dial("tcp", "localhost:10002")
	if err != nil {
		log.Fatalf("error connecting prediction server", err)
	}

	defer func() {
		device.Close()
		h.Close()
		predictionServerConn.Close()
	}()

	shutdown := make(chan bool, 1)
	mc := NewMindControl(h.broadcast, shutdown, device)
	mc.PredictionServerConn = predictionServerConn

	handle := NewHandle(mc)

	http.HandleFunc("/ws", h.wsPacketHandler)

	http.HandleFunc("/", handle.rootHandler)
	http.HandleFunc("/cache.manifest", handle.cacheHandler)
	http.HandleFunc("/x/", handle.commandHandler)
	http.HandleFunc("/fft/", handle.fftHandler)
	http.HandleFunc("/reset", handle.resetHandler)
	http.HandleFunc("/start", handle.startHandler)
	http.HandleFunc("/stop", handle.stopHandler)
	http.HandleFunc("/close", handle.closeHandler)
	http.HandleFunc("/save", handle.saveHandler)
	http.HandleFunc("/test", handle.testHandler)
	http.HandleFunc("/fonts/", handle.fontsHandler)
	http.HandleFunc("/js/", handle.jsHandler)
	http.HandleFunc("/css/", handle.cssHandler)
	http.HandleFunc("/img/", handle.imgHandler)
	http.HandleFunc("/data/", handle.dataHandler)
	http.HandleFunc("/mp3/", handle.mp3Handler)
	http.HandleFunc("/wav/", handle.wavHandler)
	http.HandleFunc("/bootstrap/", handle.bootstrapHandler)
	http.HandleFunc("/js/libs/", handle.libsHandler)

	go h.Run()
	go mc.Start()

	run := func(shutdown <-chan bool) {
		go http.ListenAndServe(*addr, nil)
		for {
			select {
			case <-shutdown:
				return
			}
		}
	}
	run(shutdown)

}
