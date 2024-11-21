package main

import (
	"log"
	"net"
	"sync"
)

func main() {
	conn, err := net.Dial("tcp", "a.nginx-gw.homenet:80")
	if err != nil {
		log.Fatal("failed connecting to tcp server: %v", err)
	}
	tcpConn := conn.(*net.TCPConn)

	var wg sync.WaitGroup
	wg.Add(2)
	// reader gor
	rbuf := make([]byte, 2<<15)
	go func() {
		for {
			_, err := tcpConn.Read(rbuf)
			if err != nil {
				log.Printf("failed reading from conn: %v\n", err)
				return
			}
			log.Printf("got message: %s\n", string(rbuf))
		}
	}()

	go func() {
		conn.Write([]byte("GET / HTTP/1.1\r\nHost: sugandi.com\r\n\r\nGET / HTTP/1.1\r\nHost: sugandi.com\r\n\r\n"))
	}()

	wg.Wait()
}
