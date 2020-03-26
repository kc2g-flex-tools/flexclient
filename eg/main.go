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
	cli, err := flexclient.NewFlexClient(os.Args[1])
	if err != nil {
		panic(err)
	}

	go func() {
		messages := make(chan flexclient.Message)
		cli.SetMessageChan(messages)
		for msg := range messages {
			fmt.Printf("M[%s]%s\n", msg.SenderHandle, msg.Message)
		}
	}()

	go func() {
		updates := make(chan flexclient.StateUpdate)
		cli.SetStateChan(updates)
		for upd := range updates {
			fmt.Printf("S[%s]%s: %v -> %v\n", upd.SenderHandle, upd.Object, upd.Updated, upd.CurrentState)
		}
	}()

	go func() {
		file, err := os.Create("out.raw")
		if err != nil {
			panic(err)
		}

		vitaPackets := make(chan flexclient.VitaPacket)
		cli.SetVitaChan(vitaPackets)
		for pkt := range vitaPackets {
			if pkt.Preamble.Class_id.PacketClassCode == 0x03e3 {
				file.Write(pkt.Payload)
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		cli.Run()
		wg.Done()
	}()

	cli.StartUDP()
	cli.SendAndWait("sub slice all")

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		_ = <-c
		fmt.Println("Exit on SIGINT")
		cli.Close()
	}()

	go func() {
		lines := bufio.NewScanner(os.Stdin)
		for lines.Scan() {
			res := cli.SendAndWait(lines.Text())
			fmt.Println(res)
		}
		cli.Close()
	}()

	wg.Wait()
}
