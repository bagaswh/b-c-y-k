package main

import (
	"errors"
	"flag"
	"go-discord/pkg/message"
	"io"
	"net"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TODO:
// 1. Message broadcasting (global groups)
// 2. People can start own groups
// 3. A person can "whisper" to other person
// 4. Rate limiting
// 5. Performance optimization: clients in map or slice (use BinarySearch or other fast slice algo to remove)

type Client struct {
	Conn *net.TCPConn
}

type Message struct{}

var clients []*Client

var (
	addr     = flag.String("flag", ":7811", "address the server listens to")
	logLevel = flag.String("log-level", "info", "log level")
)

func setLogLevel() {
	switch *logLevel {
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}
}

func setupZeroLog() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	setLogLevel()
}

func main() {
	flag.Parse()
	setupZeroLog()

	tcpAddr, resolveTcpAddrErr := net.ResolveTCPAddr("tcp", *addr)
	if resolveTcpAddrErr != nil {
		log.Fatal().Msgf("failed resolving address %s to TCP address", *addr)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal().Msgf("cannot listen to addr %s: %v", *addr, err)
	}

	log.Info().Str("listen_addr", tcpAddr.String()).Msg("listening")

	for {
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			log.Err(acceptErr).Msg("failed to accept connection")
		}
		client := &Client{
			Conn: conn,
		}
		clients = append(clients, client)
		log.Debug().Str("new_client", conn.RemoteAddr().String()).Msg("new connection")
		go handleConnection(conn)
	}

}

// func server(listener *net.Listener) {

// }

const MiB = 1048576

// The addition is to account for: null termination and send signifier
const MessageBufferMaxSize = (8 * MiB) + 2

var MaxBufferSizeExceededError = errors.New("max buffer size exceeded")

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type MessageBuffer struct {
	buf *[]byte
	off int
}

// readConn efficiently reads connection with max buffer size upper bound

// arr = underlying arr
// b -> arr
// &b -> b -> arr
// *b -> arr

func readConn(conn *net.TCPConn, b *[]byte, cb func(b []byte)) error {
	// fmt.Println(len(*b), cap(*b), "before read loop")
	endOfText := false
	for {
		if endOfText {
			// reset pointer to 0
			*b = (*b)[:0]
			endOfText = false
		}
		derefb := *b
		sLo := len(derefb)
		sHi := cap(derefb)
		n, err := conn.Read(derefb[sLo:sHi])
		derefb = derefb[:len(derefb)+n]
		// fmt.Println(n, err, string(derefb), string((*b)[:len(*b)+n]), "after conn.Read")
		if err != nil {
			return err
		}

		// // null termination for efficiency
		// // so we don't have to clear the buffer when we are done with reading message
		// if len(derefb) > 0 {
		// 	derefb[len(derefb)] = 0
		// }

		// fmt.Println(len(derefb), cap(derefb), "OUT DA if len(derefb) == cap(derefb)")
		if len(derefb) == cap(derefb) {
			if cap(derefb) >= MessageBufferMaxSize {
				return MaxBufferSizeExceededError
			}

			// 1918476 B
			// 3836952 B
			// 7673904 B
			// 8388608 B

			// create new slice ourself
			// use Go rule capacity growing
			newCap := 0
			if cap(derefb) < 256 {
				newCap = cap(derefb) * 2
			} else {
				newCap = (cap(derefb) + 768) / 4
			}
			newCap = min(MessageBufferMaxSize, newCap)
			log.Trace().Msgf("new allocation from %d to %d, msg=%s", cap(derefb), newCap, string(derefb))
			newSlice := make([]byte, len(derefb), newCap)
			copy(newSlice, derefb)
			*b = newSlice
			derefb = *b

			// fmt.Println(len(derefb), cap(derefb), "IN DA if len(derefb) == cap(derefb)")
		}
		// cb(derefb[0:])
		// fmt.Printf("message: b=%#+v s=%s\n", derefb, derefb)
		if derefb[len(derefb)-1] == message.LineTerminationByte {
			cb(derefb[0:])
			// fmt.Printf("%s\n", derefb[0:])
			endOfText = true
		}
	}
}

func handleConnection(conn *net.TCPConn) {
	defer conn.Close()

	logger := log.With().Str("remote_addr", conn.RemoteAddr().String()).Logger()

	b := make([]byte, 2)
	for {
		err := readConn(conn, &b, func(msg []byte) {
			logger.Debug().Msgf("got message %s", string(msg))
			for _, client := range clients {
				_, connWriteErr := client.Conn.Write(msg)
				if connWriteErr != nil {
					logger.Debug().Str("client_remote_addr", client.Conn.RemoteAddr().String()).Err(connWriteErr).Msg("failed writing to client connection")
				}
				logger.Debug().Str("client_remote_addr", client.Conn.RemoteAddr().String()).Msg("wrote to client")
			}
		})
		if err != nil {
			if err == io.EOF {
				logger.Debug().Msg("client closing")
				return
			}
			logger.Debug().Err(err).Msg("failed to read connection")
		}
	}
}
