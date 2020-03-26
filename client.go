package flexclient

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/krippendorf/flexlib-go/vita"
)

type FlexClient struct {
	sync.RWMutex
	tcpConn       net.Conn
	udpConn       *net.UDPConn
	lines         *bufio.Scanner
	state         State
	version       string
	handle        string
	cmdIndex      uint32
	messages      chan Message
	subscriptions []Subscription
	cmdResults    map[uint32]chan CmdResult
	vitaPackets   chan VitaPacket
}

type Message struct {
	SenderHandle string
	Message      string
}

type CmdResult struct {
	Serial  uint32
	Error   uint32
	Message string
}

func (c CmdResult) String() string {
	return fmt.Sprintf("[%d]%08x %s", c.Serial, c.Error, c.Message)
}

type StateUpdate struct {
	SenderHandle string
	Object       string
	Updated      Object
	CurrentState Object
}

type Subscription struct {
	Prefix  string
	Updates chan StateUpdate
}

type VitaPacket struct {
	Preamble *vita.VitaPacketPreamble
	Payload  []byte
}

type Object map[string]string
type State map[string]Object

func NewFlexClient(dst string) (*FlexClient, error) {
	tcpConn, err := net.Dial("tcp", dst)
	if err != nil {
		return nil, fmt.Errorf("%w connecting to %s", err, dst)
	}

	return &FlexClient{
		tcpConn:       tcpConn,
		lines:         bufio.NewScanner(tcpConn),
		state:         State{},
		cmdResults:    map[uint32]chan CmdResult{},
		subscriptions: []Subscription{},
	}, nil
}

func (f *FlexClient) SendCmd(cmd string) uint32 {
	f.Lock()
	defer f.Unlock()
	for {
		f.cmdIndex += 1
		// On the off chance that we've wrapped around and the four-billion-previous
		// command is still outstanding, increment until we find a free slot.
		_, found := f.cmdResults[f.cmdIndex]
		if !found {
			break
		}
	}
	fmt.Fprintf(f.tcpConn, "C%d|%s\n", f.cmdIndex, cmd)
	return f.cmdIndex
}

func (f *FlexClient) SendAndWait(cmd string) CmdResult {
	idx := f.SendCmd(cmd)
	f.Lock()
	f.cmdResults[idx] = make(chan CmdResult)
	f.Unlock()

	result := <-f.cmdResults[idx]

	f.Lock()
	delete(f.cmdResults, idx)
	f.Unlock()
	return result
}

func (f *FlexClient) udpPort() int {
	return f.udpConn.LocalAddr().(*net.UDPAddr).Port
}

func (f *FlexClient) StartUDP() error {
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return fmt.Errorf("%w binding to UDP port", err)
	}
	f.udpConn = udpConn

	res := f.SendAndWait(fmt.Sprintf("client udpport %d", f.udpPort()))
	if res.Error != 0 {
		return fmt.Errorf("%08x setting client udpport (%s)", res.Error, res.Message)
	}

	go f.runUDP()
	return nil
}

func (f *FlexClient) Run() {
	defer func() {
		if f.messages != nil {
			close(f.messages)
		}
		for _, s := range f.subscriptions {
			close(s.Updates)
		}
		for _, c := range f.cmdResults {
			close(c)
		}
		f.udpConn.Close()
	}()

	f.runTCP()
}

func (f *FlexClient) runTCP() {
	for f.lines.Scan() {
		f.parseLine(f.lines.Text())
	}
}

func (f *FlexClient) runUDP() {
	var pkt [64000]byte
	for {
		n, err := f.udpConn.Read(pkt[:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "udp: %#v\n", err)
			switch e := err.(type) {
			case net.Error:
				if e.Temporary() {
					continue
				} else {
					break
				}
			default:
				break
			}
		}

		f.parseUDP(pkt[:n])
	}
}

func (f *FlexClient) parseUDP(pkt []byte) {
	f.RLock()
	dchan := f.vitaPackets
	f.RUnlock()
	if dchan == nil {
		return
	}

	err, preamble, payload := vita.ParseVitaPreamble(pkt)
	if err == nil {
		dchan <- VitaPacket{preamble, payload}
	} else {
		fmt.Fprintf(os.Stderr, "vita parse err %s\n", err.Error())
	}
}

func (f *FlexClient) parseLine(line string) {
	if len(line) == 0 {
		return
	}

	switch line[0] {
	case 'V':
		f.parseVersion(line[1:])
	case 'H':
		f.parseHandle(line[1:])
	case 'M':
		f.parseMessage(line[1:])
	case 'S':
		f.parseState(line[1:])
	case 'R':
		f.parseCmdResult(line[1:])
	default:
		fmt.Fprintln(os.Stderr, "Unknown line:", line)
	}
}

func (f *FlexClient) parseVersion(line string) {
	f.Lock()
	f.version = line
	f.Unlock()
}

func (f *FlexClient) parseHandle(line string) {
	f.Lock()
	f.handle = line
	f.Unlock()
}

func (f *FlexClient) parseMessage(line string) {
	f.RLock()
	dchan := f.messages
	f.RUnlock()

	if dchan != nil {
		pipeIdx := strings.IndexByte(line, '|')
		msg := Message{
			SenderHandle: line[:pipeIdx],
			Message:      line[pipeIdx+1:],
		}
		dchan <- msg
	}
}

func (f *FlexClient) parseState(line string) {
	f.Lock()
	defer f.Unlock()

	pipeIdx := strings.IndexByte(line, '|')
	handle := line[:pipeIdx]
	status := line[pipeIdx+1:]

	object := ""
	set := Object{}

	parts := strings.Split(status, " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		eqIdx := strings.IndexByte(part, '=')
		if eqIdx == -1 {
			if i == len(parts)-1 && part == "removed" {
				f.state[object] = nil
			} else if parts[0] == "client" && i == 2 && part == "connected" {
				// don't include "connected" in the object name
			} else if parts[0] == "client" && i == 2 && part == "disconnected" {
				f.state[object] = nil
			} else {
				if object != "" {
					object += " "
				}
				object += part
			}
		} else {
			if f.state[object] == nil {
				f.state[object] = map[string]string{}
			}
			key := part[0:eqIdx]
			val := part[eqIdx+1:]
			f.state[object][key] = val
			set[key] = val
		}
	}

	for _, sub := range f.subscriptions {
		if strings.HasPrefix(object, sub.Prefix) {
			sub.Updates <- StateUpdate{
				SenderHandle: handle,
				Object:       object,
				Updated:      set,
				CurrentState: f.state[object],
			}
		}
	}
}

func (f *FlexClient) parseCmdResult(line string) {
	parts := strings.Split(line, "|")
	ser, _ := strconv.ParseUint(parts[0], 10, 32)
	err, _ := strconv.ParseUint(parts[1], 16, 32)

	res := CmdResult{
		Serial:  uint32(ser),
		Error:   uint32(err),
		Message: parts[2],
	}

	f.RLock()
	dchan := f.cmdResults[res.Serial]
	f.RUnlock()

	if dchan != nil {
		dchan <- res
	}
}

func (f *FlexClient) Close() error {
	return f.tcpConn.Close()
}

func (f *FlexClient) SetMessageChan(ch chan Message) {
	f.Lock()
	defer f.Unlock()
	f.messages = ch
}

func (f *FlexClient) Subscribe(sub Subscription) Subscription {
	f.Lock()
	defer f.Unlock()
	f.subscriptions = append(f.subscriptions, sub)
	return sub
}

func (f *FlexClient) Unsubscribe(sub Subscription) {
	f.Lock()
	defer f.Unlock()
	for i, s := range f.subscriptions {
		if s == sub {
			f.subscriptions = append(f.subscriptions[:i], f.subscriptions[i+1:]...)
		}
	}
}

func (f *FlexClient) SetVitaChan(ch chan VitaPacket) {
	f.Lock()
	defer f.Unlock()
	f.vitaPackets = ch
}

func (f *FlexClient) getObject(key string) (Object, bool) {
	val, ok := f.state[key]
	if !ok {
		return nil, false
	}
	cpy := map[string]string{}
	for k, v := range val {
		cpy[k] = v
	}
	return cpy, true
}

func (f *FlexClient) GetObject(key string) (Object, bool) {
	f.RLock()
	defer f.RUnlock()
	return f.getObject(key)
}

func (f *FlexClient) FindObjects(pfx string) State {
	f.RLock()
	defer f.RUnlock()
	ret := State{}

	for key, _ := range f.state {
		if strings.HasPrefix(key, pfx) {
			obj, ok := f.getObject(key)
			if ok {
				ret[key] = obj
			}
		}
	}
	return ret
}
