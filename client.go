package flexclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type FlexClient struct {
	sync.RWMutex

	tcpConn        net.Conn
	udpConn        *net.UDPConn
	lines          *bufio.Scanner
	radioIP        string
	udpDest        net.Addr
	udpListenPort  int
	state          State
	version        string
	handle         string
	cmdIndex       uint32
	messages       chan Message
	subscriptions  []Subscription
	cmdResults     map[uint32]chan CmdResult
	vitaPackets    chan VitaPacket
	meterChan      chan MeterReport
	meterTemplates map[string]MeterReport
	stateNotify    chan struct{}
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

type ResultHandle struct {
	Serial uint32
	C      chan CmdResult
	fc     *FlexClient
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

type Object map[string]string
type State map[string]Object

func (o Object) Copy() Object {
	ret := Object{}
	for k, v := range o {
		ret[k] = v
	}
	return ret
}

func NewFlexClient(dst string) (*FlexClient, error) {
	var ip, tcpPort string

	if strings.HasPrefix(dst, ":discover:") {
		radio, err := Discover(strings.TrimPrefix(dst, ":discover:"))
		if err != nil {
			return nil, fmt.Errorf("discovery: %w", err)
		}
		ip = radio["ip"]
		tcpPort = radio["port"]
	} else {
		idx := strings.IndexByte(dst, ':')
		if idx == -1 {
			ip = dst
			tcpPort = "4992"
		} else {
			ip = dst[0:idx]
			tcpPort = dst[idx+1:]
		}
	}

	tcpConn, err := net.Dial("tcp", ip+":"+tcpPort)
	if err != nil {
		return nil, fmt.Errorf("%w connecting to %s", err, dst)
	}

	udpDest, err := net.ResolveUDPAddr("udp", ip+":4991")
	if err != nil {
		return nil, fmt.Errorf("%w resolving UDP destination", err)
	}

	return &FlexClient{
		radioIP:        dst,
		udpDest:        udpDest,
		udpListenPort:  0,
		tcpConn:        tcpConn,
		lines:          bufio.NewScanner(tcpConn),
		state:          State{},
		cmdResults:     map[uint32]chan CmdResult{},
		subscriptions:  []Subscription{},
		meterTemplates: map[string]MeterReport{},
	}, nil
}

// Assumes already locked
func (f *FlexClient) sendCmd(cmd string) uint32 {
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

func (f *FlexClient) SendCmd(cmd string) uint32 {
	f.Lock()
	defer f.Unlock()
	return f.sendCmd(cmd)
}

func (f *FlexClient) SendNotify(cmd string) ResultHandle {
	f.Lock()
	defer f.Unlock()
	idx := f.sendCmd(cmd)
	f.cmdResults[idx] = make(chan CmdResult)
	return ResultHandle{
		fc:     f,
		Serial: idx,
		C:      f.cmdResults[idx],
	}
}

func (f *FlexClient) SendAndWait(cmd string) CmdResult {
	h := f.SendNotify(cmd)
	result := <-h.C
	h.Close()
	return result
}

// SendAndWaitContext sends a command and waits for the result, or returns an error if the
// context is canceled before the result is received.
func (f *FlexClient) SendAndWaitContext(ctx context.Context, cmd string) (CmdResult, error) {
	h := f.SendNotify(cmd)
	select {
	case result := <-h.C:
		h.Close()
		return result, nil
	case <-ctx.Done():
		h.Close()
		return CmdResult{}, ctx.Err()
	}
}

func (h *ResultHandle) Close() {
	h.fc.Lock()
	defer h.fc.Unlock()
	if h.fc.cmdResults != nil {
		delete(h.fc.cmdResults, h.Serial)
	}
}

func (f *FlexClient) udpPort() int {
	return f.udpConn.LocalAddr().(*net.UDPAddr).Port
}

func (f *FlexClient) SetUDPPort(port int) {
	f.udpListenPort = port
}

// Makes a UDP listen socket and asks the radio to deliver VITA-49 packets
// to it. Must be done before calling RunUDP.
func (f *FlexClient) InitUDP() error {
	udpConn, err := net.ListenUDP(
		"udp",
		&net.UDPAddr{
			Port: f.udpListenPort,
		},
	)
	if err != nil {
		return fmt.Errorf("%w binding to UDP port", err)
	}
	f.udpConn = udpConn

	res := f.SendAndWait(fmt.Sprintf("client udpport %d", f.udpPort()))
	if res.Error != 0 {
		return fmt.Errorf("%08x setting client udpport (%s)", res.Error, res.Message)
	}

	return nil
}

// Calls InitUDP(), returning error if any, then calls RunUDP() in a goroutine.
// Provided for interface compatibility with existing clients, and for simplicity for
// those that don't care about RT scheduling.
func (f *FlexClient) StartUDP() error {
	err := f.InitUDP()
	if err != nil {
		return err
	}
	go f.RunUDP()
	return nil
}

func (f *FlexClient) Run() {
	defer func() {
		f.Lock()
		defer f.Unlock()
		if f.messages != nil {
			close(f.messages)
			f.messages = nil
		}
		for _, s := range f.subscriptions {
			close(s.Updates)
		}
		f.subscriptions = nil
		for _, c := range f.cmdResults {
			close(c)
		}
		f.cmdResults = nil
		if f.udpConn != nil {
			f.udpConn.Close()
			f.udpConn = nil
		}
	}()

	f.runTCP()
}

func (f *FlexClient) runTCP() {
	for f.lines.Scan() {
		f.parseLine(f.lines.Text())
	}
}

// Requires InitUDP() to have been called.
// This should be run inside a goroutine, but doesn't create one on its own, so that
// the caller may create their own and acquire realtime priority for it if they wish.
func (f *FlexClient) RunUDP() {
	var pkt [64000]byte
	for {
		n, err := f.udpConn.Read(pkt[:])
		if err != nil {
			log.Printf("flexclient: UDP read error: %v", err)
			var netErr net.Error
			if errors.As(err, &netErr) {
				// For network errors, only exit on timeout/deadline
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					continue
				}
			}
			goto out
		} else {
			f.parseUDP(pkt[:n])
		}
	}
out:
	if f.vitaPackets != nil {
		close(f.vitaPackets)
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
		log.Printf("flexclient: unknown protocol line: %s", line)
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
		if pipeIdx == -1 {
			log.Printf("flexclient: malformed message line: %s", line)
			return
		}
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

	handle, status, _ := strings.Cut(line, "|")
	firstWord, remainder, _ := strings.Cut(status, " ")

	switch firstWord {
	case "meter":
		f.parseMeterState(handle, remainder)
	default:
		f.parseGenericState(handle, status)
	}
}

// For the given object/key pairs, the value should have
// the character 0x7f replaced with 0x20 (space).
var replace7f = map[[2]string]bool{
	{"client", "station"}: true,
	{"memory", "owner"}:   true,
	{"memory", "group"}:   true,
	{"memory", "name"}:    true,
	{"spot", "callsign"}:  true,
	{"spot", "comment"}:   true,
}

func (f *FlexClient) parseGenericState(handle, status string) {
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
			key := part[0:eqIdx]
			val := part[eqIdx+1:]
			if replace7f[[2]string{parts[0], key}] {
				val = strings.ReplaceAll(val, "\x7f", " ")
			}
			set[key] = val
		}
	}

	f.updateState(handle, object, set)
}

// Assumes locked
func (f *FlexClient) updateState(updatedBy string, object string, changes Object) {
	if f.state[object] == nil {
		f.state[object] = map[string]string{}
	}

	for k, v := range changes {
		if strings.HasPrefix(object, "usb_cable ") && strings.HasPrefix(k, "data:") {
			// Special case:  data received from USB cables is passed to any subscriptions
			// but not persisted in state.
			continue
		}
		f.state[object][k] = v
	}

	for _, sub := range f.subscriptions {
		if strings.HasPrefix(object, sub.Prefix) {
			sub.Updates <- StateUpdate{
				SenderHandle: updatedBy,
				Object:       object,
				Updated:      changes,
				CurrentState: f.state[object].Copy(),
			}
		}
	}
	if f.stateNotify != nil {
		select {
		case f.stateNotify <- struct{}{}:
		default:
		}
	}
}

func (f *FlexClient) SetStateNotify(ch chan struct{}) {
	f.Lock()
	defer f.Unlock()
	f.stateNotify = ch
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
	if sub.Updates != nil {
		close(sub.Updates)
		sub.Updates = nil
	}
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

func (f *FlexClient) findObjects(pfx string) State {
	ret := State{}

	for key := range f.state {
		if strings.HasPrefix(key, pfx) {
			obj, ok := f.getObject(key)
			if ok {
				ret[key] = obj
			}
		}
	}
	return ret
}

func (f *FlexClient) FindObjects(pfx string) State {
	f.RLock()
	defer f.RUnlock()
	return f.findObjects(pfx)
}

func (f *FlexClient) ClientID() string {
	f.RLock()
	defer f.RUnlock()
	return f.handle
}
