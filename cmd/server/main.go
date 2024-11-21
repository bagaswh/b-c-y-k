package main

import (
	"bytes"
	"errors"
	"flag"
	"go-discord/pkg/message"
	pkgzerolog "go-discord/pkg/zerolog"
	"net"
	"net/http"
	"runtime"
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
// 1. Prevent "slow-loris" attack where clients keep stuffing bytes without ever sending the termination byte

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

	broadcasterGoroutinesNum = flag.Int("broadcaster-goroutine-num", runtime.NumCPU(), "number of broadcaster goroutines")
)

var (
	clientsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "godc_clients_connected_total",
		Help: "Total number of connected clients",
	})
	broadcastsInflightCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "godc_broadcasts_inflight",
		Help: "Number of currently in-flight broadcasts",
	})
	broadcastsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "godc_broadcasts_total",
		Help: "Total number of broadcasts",
	})
	broadcastsClientsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "godc_broadcasts_clients_total",
		Help: "Total number of broadcasted clients",
	})
)

// func takeN(s string, n int) string

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
		BroadcastGoroutinesNum:     *broadcasterGoroutinesNum,
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

	logger := log.With().Str("client", conn.RemoteAddr().String()).Logger()

	b := make([]byte, 2)
	for {
		err := s.readClient(c, &b, func(msg []byte) {
			broadcastsInflightCount.Inc()

			a := time.Now()
			s.BroadcastAll(msg)
			logger.Info().Msgf("broadcast completed in %v", time.Since(a))

			broadcastsTotal.Inc()
			broadcastsInflightCount.Dec()
		})
		if err != nil {
			logger.Debug().
				Str("last_message", string(b[:])).
				Err(err).
				Msg("failed to read connection, removing  client")
			removeClientErr := s.RemoveClient(c)
			if removeClientErr != nil {
				logger.Err(removeClientErr).Msg("remove client error")
			}
			return
		}
	}
}

func (s *Server) AddClient(c *Client) {
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
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

	cid := c.ID()
	delete(s.ClientsMap, cid)
	sliceIndex := s.ClientIDToSliceIndexMap[cid]

	s.NilClientsSliceIndexMap[sliceIndex] = struct{}{}

	// will be garbage-collected
	s.ClientsSlice[sliceIndex] = nil

	delete(s.ClientIDToSliceIndexMap, cid)

	// unlock before closing the connection
	// so the critical section is narrowed down
	// since Conn.Close() can take long time, holding the lock
	s.mu.Unlock()

	clientsCount.Set(float64(len(s.ClientsMap)))

	connCloseErr := c.Conn.Close()
	if connCloseErr != nil {
		return connCloseErr
	}

	return nil
}

func (s *Server) GetClientSeat() int {
	m := s.NilClientsSliceIndexMap
	for i := range m {
		return i
	}
	return -1
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
				Msgf("broadcaster")

			defer wg.Done()

			s.mu.RLock()
			defer s.mu.RUnlock()

			goIdx := i

			for i := clientsRange[0]; i < clientsRange[1]; i++ {
				c := s.ClientsSlice[i]
				if c == nil {
					continue
				}
				var connWriteErr error
				// _, connWriteErr := c.Conn.Write(msg)
				if connWriteErr != nil {
					s.Logger.
						Err(connWriteErr).
						Str("client", c.Addr()).
						Int("go", goIdx).
						Msg("sending to cliient")
				}

			}
		}(clientsRange)

		lo = hi
	}
	wg.Wait()
}

func (s *Server) SendTo(target *Client, msg []byte) {

}

const KiB = 1024
const MessageBufferMaxSize = (4 * KiB)

var ErrorMaxBufferSizeExceeded = errors.New("max buffer size exceeded")

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// readClient efficiently reads connection with max buffer size upper bound.
//
// It's efficient because it reuses and lazily allocates buffer.
func (s *Server) readClient(client *Client, b *[]byte, cb func(b []byte)) error {
	conn := client.Conn
	eot := false
	// ensure the length of slice is 0
	// to begin filling array from the beginning,
	// instead of from the initial slice length
	*b = (*b)[:0]
	for {
		if eot {
			eot = false
		}
		sLo := len(*b)
		sHi := cap(*b)
		n, err := conn.Read((*b)[sLo:sHi])
		if err != nil {
			return err
		}

		*b = (*b)[:len(*b)+n]
		newData := (*b)[sLo:]
		// Before this change, I assumed that all TCP streams that come
		// are full message. In reality,
		// stream could arrive with incomplete message.
		// And this is perfectly valid case, my server needs to handle it.
		//
		// Let's take an example.
		// We can establish TCP connection to NGINX server
		// and begin sending incomplete streams.
		// Say, in one write() call we send "GET", the next is " / H", the next is "TTP/1.1"
		// It's perfectly valid way to stream bytes to the server since NGINX will not treat any
		// complete call to recv() as a valid message. Rather, it buffers the message until some condition is true.
		// After that NGINX can start begin to parse/process the message.
		term := bytes.IndexByte(newData, message.TerminationByte)
		if term >= 0 {
			restFromIdx := sLo + term
			cb((*b)[0:restFromIdx])

			var rest []byte
			if (len(*b) - 1) > restFromIdx {
				rest = (*b)[restFromIdx+1:]
			} else {
				rest = []byte{}
			}
			s.Logger.Debug().
				Str("message cb'ed", string((*b)[0:restFromIdx])).
				Str("newData", string(newData)).
				Int("term", term).
				Str("rest", string(rest)).
				Int("restFromIdx", restFromIdx).
				Msg("readClient(): term>=0")
			// reset pointer up to len(rest) to make
			// space for the rest of the slice after termination
			*b = (*b)[:len(rest)]
			// move the rest of the slice to beginning again, reusing buffer
			copy(*b, rest)
			s.Logger.Debug().
				Str("b", string(*b)).
				Int("len(b)", len(*b)).
				Msg("b after reset")
		}

		// entire write: abcdefghijklmn\x03akdhasjhas\x03askdnsadnas\x03sugandi
		//
		// derefb[0:]: abcdefghijklmn\x03a
		// filled: ijklmn\x03a
		// term: 6
		// rest: a
		// cb slice: abcdefghijklmn
		// restFromIdx: 8+6=14
		// len(*b): 1
		// *b: a
		//
		// derefb[0:]: abcdefghijklmn\x03akdhasjhas\x03askdns
		// filled: kdhasjhas\x03askdns
		// term: 9
		// cb slice: akdhasjhas
		// lastFoundTermindex: 10
		//
		// derefb[0:]: abcdefghijklmn\x03akdhasjhas\x03askdnsadnas\x03sugandi
		// filled: adnas\x03
		// term: 5
		// cb slice: askdnsadnas
		// lastFoundTermIndex: 32+5=37

		if len(*b) == cap(*b) {
			if cap(*b) >= MessageBufferMaxSize {
				return ErrorMaxBufferSizeExceeded
			}

			// grow slice
			newCap := 0
			if cap(*b) < 256 {
				newCap = cap(*b) * 2
			} else {
				newCap = cap(*b) + ((cap(*b) + 768) / 4)
			}
			newCap = min(MessageBufferMaxSize, newCap)
			s.Logger.Trace().
				Str("client", client.Addr()).
				Msgf("new allocation from %d to %d msg=%s", cap(*b), newCap, string((*b)[0:]))
			newSlice := make([]byte, len(*b), newCap)
			copy(newSlice, *b)
			*b = newSlice
		}

	}
}
