package main

import (
	"flag"
	"fmt"
	pkgzerolog "go-discord/pkg/zerolog"
	"time"

	"github.com/rs/zerolog/log"

	"math/rand/v2"
	"net"
	"sync"
)

var (
	server      = flag.String("server", "127.0.0.1:7811", "client address to connect to")
	concurrency = flag.Int("c", 10, "how many clients to spawn")
)

func main() {
	flag.Parse()
	log.Logger = *pkgzerolog.SetupZeroLog("debug")

	//  := *server
	c := *concurrency

	clientCounter := make(chan int, c)
	clientCount := 0

	var wg sync.WaitGroup
	for i := 0; i < c; i++ {
		wg.Add(1)
		go func(cName int) {
			// log.Info().Int("client", cName).Msg("client starting")
			clientErr := startClient(cName, clientCounter)
			if clientErr != nil {
				log.Info().Int("client", cName).Err(clientErr)
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			fmt.Println("client count", clientCount)
			time.Sleep(1 * time.Second)
		}
	}()

	// for range clientCounter {
	// 	clientCount++
	// }

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

func startClient(name int, clientCounter chan<- int) error {
	conn, err := net.Dial("tcp", *server)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v [client=%v]", err, name)
	}
	defer conn.Close()
	// clientCounter <- 1
	for {
		b := make([]byte, 512)
		_, err := conn.Read(b)
		if err != nil {
			log.Printf("error reading from conn: %v", err)
			return err
		}
		// fmt.Printf(">> IN [%v]: %v\n", name, string(b))
		// fmt.Printf("user: %s\n", b)
	}
	// fmt.Println("done")
	// return nil
	// msg := []byte(randomString(512))
	// for {
	// 	_, writeErr := conn.Write(msg)
	// 	if writeErr != nil {
	// 		return fmt.Errorf("failed to write to server: %v", writeErr)
	// 	}
	// }
}
