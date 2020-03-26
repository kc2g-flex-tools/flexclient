package flexclient

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

type FlexClient struct {
	sync.RWMutex
	conn         net.Conn
	lines        *bufio.Scanner
	state        map[string]map[string]string
	version      string
	handle       string
	cmdIndex     uint32
	messages     chan Message
	stateUpdates chan StateUpdate
	cmdResults   map[uint32]chan CmdResult
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

type StateUpdate struct {
	SenderHandle string
	Object       string
	Updated      map[string]string
	CurrentState map[string]string
}

func NewFlexClient(dst string) (*FlexClient, error) {
	conn, err := net.Dial("tcp", dst)
	if err != nil {
		return nil, fmt.Errorf("%w connecting to %s", err, dst)
	}

	return &FlexClient{
		conn:       conn,
		lines:      bufio.NewScanner(conn),
		state:      map[string]map[string]string{},
		cmdResults: map[uint32]chan CmdResult{},
	}, nil
}

func (f *FlexClient) SendCmd(cmd string) uint32 {
	f.Lock()
	defer f.Unlock()
	f.cmdIndex += 1
	fmt.Fprintf(f.conn, "C%d|%s\n", f.cmdIndex, cmd)
	return f.cmdIndex
}

func (f *FlexClient) SendAndWait(cmd string) CmdResult {
	idx := f.SendCmd(cmd)
	fmt.Println("Sent", idx)
	f.Lock()
	f.cmdResults[idx] = make(chan CmdResult)
	f.Unlock()
	fmt.Printf("Wait queue: %+v\n", f.cmdResults)

	result := <-f.cmdResults[idx]
	return result
}

func (f *FlexClient) Run() {
	defer func() {
		close(f.messages)
		close(f.stateUpdates)
		for _, c := range f.cmdResults {
			close(c)
		}
	}()

	for f.lines.Scan() {
		f.ParseLine(f.lines.Text())
	}
}

func (f *FlexClient) ParseLine(line string) {
	switch line[0] {
	case 'V':
		f.ParseVersion(line[1:])
	case 'H':
		f.ParseHandle(line[1:])
	case 'M':
		f.ParseMessage(line[1:])
	case 'S':
		f.ParseState(line[1:])
	case 'R':
		f.ParseCmdResult(line[1:])
	default:
		fmt.Println("Unknown line:", line)
	}
}

func (f *FlexClient) ParseVersion(line string) {
	f.Lock()
	f.version = line
	f.Unlock()
	fmt.Println("Version:", f.version)
}

func (f *FlexClient) ParseHandle(line string) {
	f.Lock()
	f.handle = line
	f.Unlock()
	fmt.Println("Handle:", f.handle)
}

func (f *FlexClient) ParseMessage(line string) {
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

func (f *FlexClient) ParseState(line string) {
	f.Lock()
	defer f.Unlock()

	pipeIdx := strings.IndexByte(line, '|')
	handle := line[:pipeIdx]
	status := line[pipeIdx+1:]

	object := ""
	set := map[string]string{}

	parts := strings.Split(status, " ")
	for _, part := range parts {
		if part == "" {
			continue
		}
		eqIdx := strings.IndexByte(part, '=')
		if eqIdx == -1 {
			if object != "" {
				object += " "
			}
			object += part
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

	if f.stateUpdates != nil {
		f.stateUpdates <- StateUpdate{
			SenderHandle: handle,
			Object:       object,
			Updated:      set,
			CurrentState: f.state[object],
		}
	}
}

func (f *FlexClient) ParseCmdResult(line string) {
	parts := strings.Split(line, "|")
	ser, _ := strconv.ParseUint(parts[0], 10, 32)
	err, _ := strconv.ParseUint(parts[1], 16, 32)

	res := CmdResult{
		Serial:  uint32(ser),
		Error:   uint32(err),
		Message: parts[2],
	}

	fmt.Printf("CMD RES: %+v\n", res)

	f.RLock()
	dchan := f.cmdResults[res.Serial]
	f.RUnlock()

	if dchan != nil {
		dchan <- res
	}
}

func (f *FlexClient) Close() error {
	return f.conn.Close()
}

func (f *FlexClient) SetMessageChan(ch chan Message) {
	f.Lock()
	defer f.Unlock()
	f.messages = ch
}

func (f *FlexClient) SetStateChan(ch chan StateUpdate) {
	f.Lock()
	defer f.Unlock()
	f.stateUpdates = ch
}
