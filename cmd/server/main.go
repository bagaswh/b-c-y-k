package main

import (
	"errors"
	"flag"
	"go-discord/pkg/message"
	pkgzerolog "go-discord/pkg/zerolog"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TODO:
// 1. Message broadcasting (global groups)
// 2. People can start own groups
// 3. A person can "whisper" to other person
// 4. Rate limiting
// 5. Performance optimization: clients in map or slice (use BinarySearch or other fast slice algo to remove)

type ClientState byte

const (
	ClientStateDisconnected ClientState = iota
	ClientStateRemoved      ClientState = iota
)

var xxhasher = xxhash.New()

type Client struct {
	Conn *net.TCPConn
}

func (c *Client) Addr() string {
	// if c == nil || c.Conn == nil {
	// 	return ""
	// }
	return c.Conn.RemoteAddr().String()
}

func (c *Client) ID() ClientID {
	xxhasher.Reset()

	xxhasher.WriteString(c.Addr())
	idHash := ClientID(xxhasher.Sum64())
	return idHash
}

type Message struct {
	// From will be populated with sender's ID.
	From string

	Content []byte
}

var (
	addr        = flag.String("addr", ":7811", "address the server listens to")
	metricsAddr = flag.String("metrics-addr", ":2911", "address the server listens to")
	logLevel    = flag.String("log-level", "info", "log level")
)

var (
	clientsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "godc_clients_connected_total",
		Help: "Total number of connected clients",
	})
)

func main() {
	flag.Parse()
	log.Logger = *pkgzerolog.SetupZeroLog(*logLevel)

	var wg sync.WaitGroup

	tcpAddr, resolveTcpAddrErr := net.ResolveTCPAddr("tcp", *addr)
	if resolveTcpAddrErr != nil {
		log.Fatal().Msgf("failed resolving address %s to TCP address", *addr)
	}

	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal().Msgf("cannot listen to addr %s: %v", *addr, err)
	}

	log.Info().Str("listen_addr", tcpAddr.String()).Msg("listening")

	serverLogger := log.With().Str("context", "server").Logger()
	server := NewServer(listener, &Config{
		ClientRemoverGoroutinesNum: 4,
		BroadcastGoroutinesNum:     4,
	}, &serverLogger)
	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Start()
	}()

	// Prometheus
	wg.Add(1)
	go func() {
		defer wg.Done()
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	wg.Wait()
}

type Config struct {
	ClientRemoverGoroutinesNum int
	BroadcastGoroutinesNum     int
}

type ClientID uint64

type Server struct {
	Listener *net.TCPListener
	Config   *Config
	Logger   *zerolog.Logger

	ClientsSlice            []*Client
	ClientIDToSliceIndexMap map[ClientID]int
	ClientsMap              map[ClientID]*Client

	// NilClientsSliceIndexMap stores the indices of ClientsSlice that are nil.
	NilClientsSliceIndexMap map[int]struct{}

	mu sync.RWMutex
}

func NewServer(listener *net.TCPListener, config *Config, logger *zerolog.Logger) *Server {
	return &Server{
		ClientsSlice:            make([]*Client, 0),
		ClientIDToSliceIndexMap: make(map[ClientID]int),
		ClientsMap:              make(map[ClientID]*Client),
		NilClientsSliceIndexMap: make(map[int]struct{}),

		Listener: listener,
		Config:   config,
		Logger:   logger,
	}
}

func (s *Server) Start() {
	for {
		conn, acceptErr := s.Listener.AcceptTCP()
		if acceptErr != nil {
			log.Err(acceptErr).Msg("failed to accept connection")
		}
		client := &Client{
			Conn: conn,
		}
		s.AddClient(client)
		log.Debug().Str("new_client", client.Addr()).Msg("new connection")
		go s.handleClient(client)
	}
}

func (s *Server) handleClient(c *Client) {
	conn := c.Conn

	logger := log.With().Str("remote_addr", conn.RemoteAddr().String()).Logger()

	b := make([]byte, 2)
	for {
		err := readConn(conn, &b, func(msg []byte) {
			logger.Debug().Msgf("got message %s", string(msg))
			a := time.Now()
			s.BroadcastAll(msg)
			logger.Info().Msgf("broadcast completed in %v", time.Now().Sub(a))
		})
		if err != nil {
			if err == io.EOF {
				logger.Debug().Msg("client closing")
				removeClientErr := s.RemoveClient(c)
				if removeClientErr != nil {
					log.Err(removeClientErr).Str("client", c.Addr()).Msg("remove client error")
				}
				return
			}
			logger.Debug().Err(err).Msg("failed to read connection")
		}
	}
}

func (s *Server) AddClient(c *Client) {
	s.mu.Lock()
	defer func() {
		defer s.mu.Unlock()
		clientsCount.Set(float64(len(s.ClientsMap)))
	}()

	cid := c.ID()

	// reuse seat
	if len(s.NilClientsSliceIndexMap) > 0 {
		clientSeat := s.GetClientSeat()
		s.ClientsSlice[clientSeat] = c
		delete(s.NilClientsSliceIndexMap, clientSeat)
		s.ClientIDToSliceIndexMap[cid] = clientSeat

		s.Logger.Debug().Msgf("reusing seat %v for new client %v", clientSeat, c.Addr())
	} else {
		s.ClientsSlice = append(s.ClientsSlice, c)
		s.ClientIDToSliceIndexMap[cid] = len(s.ClientsSlice) - 1

		s.Logger.Debug().Msgf("client %v gets appended to ClientsSlice", c.Addr())
	}

	s.ClientsMap[cid] = c
}

func (s *Server) RemoveClient(c *Client) error {
	s.mu.Lock()
	defer func() {
		defer s.mu.Unlock()
		clientsCount.Set(float64(len(s.ClientsMap)))
	}()

	// c.State = ClientStateRemoved
	cid := c.ID()
	delete(s.ClientsMap, cid)
	sliceIndex := s.ClientIDToSliceIndexMap[cid]

	s.NilClientsSliceIndexMap[sliceIndex] = struct{}{}

	// will be garbage-collected
	s.ClientsSlice[sliceIndex] = nil

	delete(s.ClientIDToSliceIndexMap, cid)

	connCloseErr := c.Conn.Close()
	if connCloseErr != nil {
		return connCloseErr
	}

	// s.Logger.Debug().Msgf("reusing seat %v for client %v", clientSeat, c.ID())

	return nil
}

func (s *Server) GetClientSeat() int {
	m := s.NilClientsSliceIndexMap
	for i := range m {
		return i
	}
	return -1
}

func (s *Server) disposeRemovedClient() {

}

func (s *Server) BroadcastAll(msg []byte) {
	gos := s.Config.BroadcastGoroutinesNum
	var wg sync.WaitGroup
	perGoroutine := len(s.ClientsMap) / gos
	mod := len(s.ClientsMap) % gos
	addition := 0
	if mod > 0 {
		addition += 1
	}
	actualGos := min(gos, len(s.ClientsMap))
	wg.Add(actualGos)

	// gos: 4
	// addition: 1
	// pergor: 11/4: 2
	// remainder: 3
	//   - [0]: {0, 3}
	//   - []

	prevhi := 0

	// ◼◼◼◼◼ ◼◼ ◼◼ ◼◼

	var lo, hi int
	for i := 0; i < actualGos; i++ {
		hi = lo + perGoroutine + mod
		mod = 0

		clientsRange := []int{lo, hi}
		go func(clientsRange []int) {
			s.Logger.
				Debug().
				Int("gos", actualGos).
				Int("goidx", i).
				Int("pergor", perGoroutine).
				Int("mod", mod).
				Int("addition", addition).
				Int("prevhi", prevhi).
				Any("clientsRange", clientsRange).
				Msgf("debug")

			defer wg.Done()

			s.mu.RLock()
			defer s.mu.RUnlock()

			goIdx := i

			for i := clientsRange[0]; i < clientsRange[1]; i++ {
				c := s.ClientsSlice[i]
				if c == nil {
					continue
				}
				s.Logger.
					Debug().
					Str("client", c.Addr()).
					Int("go", goIdx).
					Msg("sending to client")
				_, connWriteErr := c.Conn.Write(msg)
				if connWriteErr != nil {
					s.Logger.
						Err(connWriteErr).
						Str("client", c.Addr()).
						Int("go", goIdx).
						Msg("sending to cliient")
				}
				s.Logger.
					Debug().
					Str("client", c.Addr()).
					Int("go", goIdx).
					Msg("sent to client")

			}
		}(clientsRange)

		lo = hi
	}
	wg.Wait()
}

func (s *Server) SendTo(target *Client, msg []byte) {

}

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

// readConn efficiently reads connection with max buffer size upper bound.
// It's efficient because it reuses and lazily allocates buffer.
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
		// fmt.Println("waiting for read")
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
			// fmt.Printf("cb done msg=%s\n", derefb[0:])
			// fmt.Printf("%s\n", derefb[0:])
			endOfText = true
		}
	}
}
