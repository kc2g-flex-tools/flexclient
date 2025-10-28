package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"

	"github.com/kc2g-flex-tools/flexclient"
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
		const (
			Normal       = "\x1b[0m"
			Black        = "\x1b[30m"
			Red          = "\x1b[31m"
			Green        = "\x1b[32m"
			Brown        = "\x1b[33m"
			Blue         = "\x1b[34m"
			Magenta      = "\x1b[35m"
			Cyan         = "\x1b[36m"
			LightGray    = "\x1b[37m"
			DarkGray     = "\x1b[30;1m"
			LightRed     = "\x1b[31;1m"
			LightGreen   = "\x1b[32;1m"
			LightYellow  = "\x1b[33;1m"
			LightBlue    = "\x1b[34;1m"
			LightMagenta = "\x1b[35;1m"
			LightCyan    = "\x1b[36;1m"
			White        = "\x1b[37;1m"
		)

		updates := make(chan flexclient.StateUpdate, 10)
		sub := fc.Subscribe(flexclient.Subscription{Prefix: "", Updates: updates})
		for upd := range updates {
			fmt.Printf("S[%s] %s: ", upd.SenderHandle, upd.Object)
			keys := []string{}
			for key := range upd.CurrentState {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			space := ""
			for _, key := range keys {
				var keyColor, valueColor string
				if _, ok := upd.Updated[key]; ok {
					keyColor = LightGreen
					valueColor = LightBlue
				} else {
					keyColor = Green
					valueColor = Blue
				}
				fmt.Printf("%s%s%s%s=%s%s%s", space, keyColor, key, Normal, valueColor, upd.CurrentState[key], Normal)
				space = " "
			}
			fmt.Println("")
		}
		fc.Unsubscribe(sub)
	}()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		fc.Run()
		wg.Done()
	}()

	fc.StartUDP()
	fc.SendAndWait("sub slice all")

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
