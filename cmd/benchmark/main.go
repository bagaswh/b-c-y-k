package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"sync"
)

var (
	server      = flag.String("client-addr", "127.0.0.1:7811", "client address to connect to")
	concurrency = flag.Int("c", 10, "how many clients to spawn")
)

func main() {
	flag.Parse()

	//  := *server
	c := *concurrency

	var wg sync.WaitGroup
	for i := 0; i <= c; i++ {
		wg.Add(1)
		go func(cName int) {
			clientErr := startClient(cName)
			if clientErr != nil {
				log.Printf("client %v erroring: %v\n", cName, clientErr)
			}
		}(i)
	}

	wg.Wait()
}

func randomString(n int) string {
	pool := []string{
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ`1234567890-=~12345667045636;'3l6;",
	}
	str := ""
	for i := 0; i < n; i++ {
		r := rand.IntN(len(pool))
		str += pool[r]
	}
	return str
}

func startClient(name int) error {
	conn, err := net.Dial("tcp", *server)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	defer conn.Close()
	msg := []byte(randomString(512))
	for {
		_, writeErr := conn.Write(msg)
		if writeErr != nil {
			return fmt.Errorf("failed to write to server: %v", writeErr)
		}
	}
}
