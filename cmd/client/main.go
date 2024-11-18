package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
)

var (
	server      = flag.String("client-addr", "127.0.0.1:7811", "client address to connect to")
	concurrency = flag.Int("c", 10, "how many clients to spawn")
)

func main() {
	flag.Parse()

	conn, err := net.Dial("tcp", *server)
	if err != nil {
		log.Fatalf("failed to connect to server: %v", err)
	}

	var wg sync.WaitGroup
	go func() {
		defer wg.Done()

		for {
			b := make([]byte, 512)
			_, err := conn.Read(b)
			if err != nil {
				log.Printf("error reading from conn: %v", err)
				return
			}
			// fmt.Printf("user: %s\n", b)
		}
	}()
	wg.Wait()

	for {
		var input string
		fmt.Printf("> ")
		fmt.Scanln(&input)
		fmt.Println(input)
		// conn.Write([]byte(input))
		// conn.Write([]byte{message.LineTerminationByte})
	}

}
