package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/arodland/flexclient"
)

func main() {
	dst := ":discover:"
	if len(os.Args) > 1 {
		dst = os.Args[1]
	}
	fc, err := flexclient.NewFlexClient(dst)
	if err != nil {
		panic(err)
	}

	go func() {
		messages := make(chan flexclient.Message)
		fc.SetMessageChan(messages)
		for msg := range messages {
			fmt.Printf("M[%s]%s\n", msg.SenderHandle, msg.Message)
		}
	}()

	go func() {
		updates := make(chan flexclient.StateUpdate, 10)
		sub := fc.Subscribe(flexclient.Subscription{"", updates})
		for upd := range updates {
			fmt.Printf("S[%s]%s: %v -> %v\n", upd.SenderHandle, upd.Object, upd.Updated, upd.CurrentState)
		}
		fc.Unsubscribe(sub)
	}()

	go func() {
		file, err := os.Create("out.raw")
		if err != nil {
			panic(err)
		}

		vitaPackets := make(chan flexclient.VitaPacket)
		fc.SetVitaChan(vitaPackets)
		for pkt := range vitaPackets {
			if pkt.Preamble.Class_id.PacketClassCode == 0x03e3 {
				file.Write(pkt.Payload)
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		fc.Run()
		wg.Done()
	}()

	fc.StartUDP()
	fc.SendAndWait("sub slice all")

	slices := fc.FindObjects("slice ")
	fmt.Printf("%#v\n", slices)

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		_ = <-c
		fmt.Println("Exit on SIGINT")
		fc.Close()
	}()

	go func() {
		lines := bufio.NewScanner(os.Stdin)
		for lines.Scan() {
			res := fc.SendAndWait(lines.Text())
			fmt.Println(res)
		}
		fc.Close()
	}()

	wg.Wait()
}
