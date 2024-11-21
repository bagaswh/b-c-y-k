package main

import (
	"flag"
	"fmt"
	"go-discord/pkg/message"
	pkgzerolog "go-discord/pkg/zerolog"

	"github.com/rs/zerolog/log"

	"net"
	"sync"
)

var (
	server = flag.String("server", "127.0.0.1:7811", "server address to connect to")
)

func main() {
	flag.Parse()
	log.Logger = *pkgzerolog.SetupZeroLog("debug")

	conn, err := net.Dial("tcp", *server)
	if err != nil {
		log.Fatal().Msgf("failed to connect to server: %v", err)
	}

	var wg sync.WaitGroup
	go func() {
		defer wg.Done()

		for {
			b := make([]byte, 512)
			_, err := conn.Read(b)
			if err != nil {
				log.Info().Msgf("error reading from conn: %v", err)
				return
			}
			fmt.Printf("user: %s\n", b)
		}
	}()
	wg.Wait()

	for {
		var input string
		fmt.Printf("> ")
		fmt.Scanln(&input)
		fmt.Println(input)
		conn.Write([]byte(input))
		conn.Write([]byte{message.TerminationByte})
	}

}
