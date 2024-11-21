package main

import (
	"fmt"
	"go-discord/pkg/message"
	pkgzerolog "go-discord/pkg/zerolog"
	"net"
	"sync"
	"testing"

	"github.com/rs/zerolog/log"
)

var listenerAddr net.Addr

func init() {
	log.Logger = *pkgzerolog.SetupZeroLog("debug")

	addr := "127.0.0.1:"
	tcpAddr, resolveTcpAddrErr := net.ResolveTCPAddr("tcp", addr)
	if resolveTcpAddrErr != nil {
		log.Fatal().Msgf("failed resolving address %s to TCP address", addr)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal().Msgf("cannot listen to addr %s: %v", addr, err)
	}
	listenerAddr = listener.Addr()

	serverLogger := log.With().Str("context", "server").Logger()

	go func() {
		server := NewServer(listener, &Config{
			ClientRemoverGoroutinesNum: 4,
			BroadcastGoroutinesNum:     *broadcasterGoroutinesNum,
		}, &serverLogger)
		server.Start()
	}()

}

func TestServer_readClient_terminationBytesHandling(t *testing.T) {

	conn, err := net.Dial("tcp", listenerAddr.String())
	if err != nil {
		t.Error("could not connect to server: ", err)
	}
	defer conn.Close()

	// coord := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)
	errs := []error{}
	// reader
	go func() {
		defer wg.Done()

		rbuf := make([]byte, 4096)

		n, err := conn.Read(rbuf)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to rean conn: %v", err))
		}
		_ = n
		t.Logf("%s\n", rbuf)
	}()

	// writer
	go func() {
		defer wg.Done()

		data := make([]byte, 0)
		data = append(data, []byte("abcdefghijklmn")...)
		data = append(data, message.TerminationByte)
		data = append(data, []byte("akdhasjhas")...)
		data = append(data, message.TerminationByte)
		data = append(data, []byte("askdnsadnas")...)
		data = append(data, message.TerminationByte)
		data = append(data, []byte("sugandi")...)
		_, err := conn.Write(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to write conn: %v", err))
		}
	}()

	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("got %d errors: %#v", len(errs), errs)
	}
}
