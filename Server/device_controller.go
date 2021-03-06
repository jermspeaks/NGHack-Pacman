/*OpenBCI golang server allows users to control, visualize and store data
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
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/kevinjos/gofidlib"
)

// MindControl ...
type MindControl struct {
	SerialDevice         io.ReadWriteCloser
	PredictionServerConn net.Conn
	PacketChan           chan *Packet
	savePacketChan       chan *Packet
	deltaFFT             chan [2]int
	quitGenTest          chan bool
	quitSendPackets      chan bool
	quitSave             chan bool
	quitDecodeStream     chan bool
	pauseRead            chan chan bool
	gainC                chan *[8]float64
	shutdown             chan bool
	broadcast            chan *message
	gain                 [8]float64
	saving               bool
	genTesting           bool
}

// NewMindControl ...
func NewMindControl(broadcast chan *message, shutdown chan bool, device io.ReadWriteCloser) *MindControl {
	//Set up the serial device
	return &MindControl{
		SerialDevice:     device,
		PacketChan:       make(chan *Packet),
		savePacketChan:   make(chan *Packet),
		deltaFFT:         make(chan [2]int),
		quitGenTest:      make(chan bool),
		quitSendPackets:  make(chan bool),
		quitSave:         make(chan bool),
		quitDecodeStream: make(chan bool),
		pauseRead:        make(chan chan bool),
		gainC:            make(chan *[8]float64),
		shutdown:         shutdown,
		broadcast:        broadcast,
		gain:             [8]float64{24.0, 24.0, 24.0, 24.0, 24.0, 24.0, 24.0, 24.0},
		saving:           false,
		genTesting:       false,
	}
}

// Start necessary go routines
func (mc *MindControl) Start() {
	go DecodeStream(mc.PacketChan, mc.gainC, mc.quitDecodeStream, mc.pauseRead, mc.SerialDevice)
	go mc.sendPackets()
}

// Close go routines and channels started by MindControl
func (mc *MindControl) Close() {
	mc.SerialDevice.Write([]byte{'\x73'})
	if mc.saving {
		mc.quitSave <- true
	}
	mc.SerialDevice.Close()
	mc.quitDecodeStream <- true
	close(mc.quitSendPackets)
	close(mc.quitGenTest)
	close(mc.shutdown)
}

func (mc *MindControl) saveBDF() {
	files, err := openTmpFiles(8)
	if err != nil {
		log.Println(err)
		return
	}
	defer func() {
		mc.saving = false
		for _, f := range files {
			f.Close()
		}
	}()
	for {
		select {
		case p := <-mc.savePacketChan:
			files[0].Write(p.Rchan1)
			files[1].Write(p.Rchan2)
			files[2].Write(p.Rchan3)
			files[3].Write(p.Rchan4)
			files[4].Write(p.Rchan5)
			files[5].Write(p.Rchan6)
			files[6].Write(p.Rchan7)
			files[7].Write(p.Rchan8)
		case <-mc.quitSave:
			return
		}
	}
}

func (mc *MindControl) save() {
	var file *os.File
	fileState := 1
	startTime := time.Now().UnixNano()
	for {
		select {
		case p := <-mc.savePacketChan:
			switch fileState {
			case 1:
				file, _ = openFile()
				defer func() {
					file.Close()
					mc.saving = false
				}()
				header := bytes.NewBufferString(`NanoSec,Synced,Chan1,Chan2,Chan3,Chan4,Chan5,Chan6,Chan7,Chan8,AccX,AccY,AccZ
	`)
				_, err := file.Write(header.Bytes())
				if err != nil {
					log.Println(err)
					return
				}
				fileState++
				fallthrough
			case 2:
				row := packetToCSV(startTime, p)
				_, err := file.Write(row)
				if err != nil {
					log.Println(err)
					return
				}
			}
		case <-mc.quitSave:
			return
		}
	}
}

func (mc *MindControl) sendPackets() {
	var i int

	FFTSize := 250

	filterDesign, err := gofidlib.NewFilterDesign("BpBe4/5-30", samplesPerSecond)
	if err != nil {
		log.Fatal("Error creating filter design:", err)
	}

	filter := make([]*gofidlib.Filter, 8)
	for j := 0; j < 8; j++ {
		filter[j] = gofidlib.NewFilter(filterDesign)
	}

	defer func() {
		filterDesign.Free()
		for j := 0; j < 8; j++ {
			filter[j].Free()
		}
	}()

	pbFFT := NewPacketBatcher(FFTSize)
	pbRaw := NewPacketBatcher(RawMsgSize)

	for {
		select {
		case <-mc.quitSendPackets:
			return
		case arr := <-mc.deltaFFT:
			FFTSize = arr[0]
			//FFTFreq = arr[1]
			pbFFT = NewPacketBatcher(FFTSize)
			i = 0
		case p := <-mc.PacketChan:
			p.Chan1 = filter[0].Run(p.Chan1)
			p.Chan2 = filter[1].Run(p.Chan2)
			p.Chan3 = filter[2].Run(p.Chan3)
			p.Chan4 = filter[3].Run(p.Chan4)
			p.Chan5 = filter[4].Run(p.Chan5)
			p.Chan6 = filter[5].Run(p.Chan6)
			p.Chan7 = filter[6].Run(p.Chan7)
			p.Chan8 = filter[7].Run(p.Chan8)

			if mc.saving == true {
				mc.savePacketChan <- p
			}

			pbFFT.packets[i%FFTSize] = p
			pbRaw.packets[i%RawMsgSize] = p

			if i%RawMsgSize == RawMsgSize-1 {
				pbRaw.batch()
				msg := pbRaw.Chans
				delete(msg, "Chan3")
				delete(msg, "Chan4")
				delete(msg, "Chan5")
				delete(msg, "Chan6")
				delete(msg, "Chan7")
				delete(msg, "Chan8")
				jsonPacket, err := json.Marshal(msg)
				_, err = mc.PredictionServerConn.Write(jsonPacket)
				if err != nil {
					log.Println("Error writing to prediction server", err)
				}
				pred := make([]byte, 1)
				_, err = mc.PredictionServerConn.Read(pred)
				if err != nil {
					log.Println("Error reading from prediction server", err)
				}

				dir := calcDirection(pred[0])
				log.Printf("Direction == %v\n", dir)
				mc.broadcast <- newMessage("move", dir)

				//mc.broadcast <- newMessage("raw", pbRaw.Chans)
			}

			i++

		}
	}
}

func calcDirection(pred byte) (dir map[string][]float64) {
	dir = make(map[string][]float64)

	switch pred {
	case '\x30':
		dir["none"] = []float64{0.0}
	case '\x31':
		dir["right"] = []float64{1.0}
	case '\x32':
		dir["down"] = []float64{2.0}
	case '\x33':
		dir["left"] = []float64{3.0}
	case '\x34':
		dir["up"] = []float64{4.0}
	}
	log.Println(dir)
	return dir
}

type message struct {
	Name    string
	Payload map[string][]float64
}

func newMessage(name string, payload map[string][]float64) *message {
	return &message{
		Name:    name,
		Payload: payload,
	}
}
