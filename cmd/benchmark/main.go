package main

import (
	"flag"
	"fmt"
	"go-discord/pkg/message"
	"go-discord/pkg/netiface"
	pkgzerolog "go-discord/pkg/zerolog"
	"io"
	"time"

	"github.com/rs/zerolog/log"

	"math/rand/v2"
	"net"
	"sync"
)

var (
	server      = flag.String("server", "127.0.0.1:7811", "client address to connect to")
	concurrency = flag.Int("c", 10, "how many clients to spawn")
	iface       = flag.String("iface", "", "iface to pick for communication with the server")
	chattyLevel = flag.Float64("chatty", 0.1, "how chatty are the clients")
)

type ClientChattiness struct {
	intervalBetweenMessage time.Duration
	maxMessageLength       int
	chatProbability        float64
}

const KiB = 1024
const TwoKiB = 2 * 1024

func determineClientChattines() ClientChattiness {
	minInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second
	baseMessageLength := 10

	interval := maxInterval - time.Duration(*chattyLevel*float64(maxInterval-minInterval))
	messageLength := baseMessageLength + int(*chattyLevel*TwoKiB)
	chatProbability := *chattyLevel

	return ClientChattiness{
		intervalBetweenMessage: interval,
		maxMessageLength:       messageLength,
		chatProbability:        chatProbability,
	}
}

func main() {
	flag.Parse()
	log.Logger = *pkgzerolog.SetupZeroLog("debug")

	c := *concurrency

	clientCounter := make(chan int, c)
	clientCount := 0

	localAddr, getLocalAddrErr := netiface.GetLocalAddress(*iface)
	if getLocalAddrErr != nil {
		log.Fatal().Err(getLocalAddrErr).Msgf("failed to get local address of iface %s: %v", *iface, getLocalAddrErr)
	}

	tcpAddr := localAddr.(*net.IPNet)
	dialer := &net.Dialer{
		LocalAddr: &net.TCPAddr{
			IP:   tcpAddr.IP,
			Port: 0,
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		wg.Add(c)

		a := time.Now()
		for i := 0; i < c; i++ {
			go func(cName int) {
				defer wg.Done()

				chattiness := determineClientChattines()
				clientErr := startClient(dialer, cName, chattiness, clientCounter)
				if clientErr != nil {
					log.Error().Err(clientErr).Int("client", cName).Msg("client error")
				}
			}(i)
		}
		fmt.Printf("spawned all in %v\n", time.Now().Sub(a))
	}()

	for range clientCounter {
		clientCount++
		fmt.Println("client count", clientCount)
	}

	wg.Wait()
}

var pool = []byte(
	"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ`1234567890-=~12345667045636;'3l6;",
)

func randomString(n int) string {
	str := make([]byte, n)
	for i := 0; i < n; i++ {
		r := rand.IntN(len(pool))
		str[i] = pool[r]
	}
	return string(str)
}

func connReader(conn *net.TCPConn) {
	io.Copy(io.Discard, conn)
}

func startClient(dialer *net.Dialer, name int, chattiness ClientChattiness, clientCounter chan<- int) error {
	conn, err := dialer.Dial("tcp", *server)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v [client=%v]", err, name)
	}
	defer conn.Close()
	clientCounter <- 1

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		connReader(conn.(*net.TCPConn))
	}()

	logger := log.Logger.With().Int("client", name).Logger()

	ticker := time.NewTicker(chattiness.intervalBetweenMessage)
	defer ticker.Stop()

	for range ticker.C {
		if rand.Float64() > chattiness.chatProbability {
			continue
		}

		msg := []byte("START__" + randomString(rand.IntN(chattiness.maxMessageLength)+1) + "__END")
		// msg := []byte("ABCDEFGHIJ")
		msg = append(msg, message.TerminationByte)
		logger.Debug().Msgf("PRE Write(): msglen=%v", len(msg))
		n, err := conn.Write(msg)
		logger.Debug().Msgf("POST Write(): written=%v", n)
		if err != nil {
			return fmt.Errorf("failed to send message: %v", err)
		}
	}

	wg.Wait()
	return nil
}
