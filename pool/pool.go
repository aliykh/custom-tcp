package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const defaultMaxIdleConnections = 100
const defaultMaxOpenConnections = 200
const defaultHost = "localhost"
const defaultPort = "8090"

type TcpConnPool struct {
	host string
	port string

	mu sync.Mutex

	maxIdleConn   int
	maxOpenConn   int
	numOfOpenConn int

	idleConnections map[string]*tcpConn

	requestChan chan *connRequest // ? todo
}

// The connection request
type connRequest struct {
	connChan chan *tcpConn
	errChan  chan error
}

type Option func(t *TcpConnPool)

func WithHost(host string) Option {
	return func(t *TcpConnPool) {
		t.host = host
	}
}

func WithPort(port string) Option {
	return func(t *TcpConnPool) {
		t.port = port
	}
}

func WithMaxIdleConn(size int) Option {
	return func(t *TcpConnPool) {
		t.maxIdleConn = size
	}
}

func WithMaxOpenConn(size int) Option {
	return func(t *TcpConnPool) {
		t.maxOpenConn = size
	}
}

// CreateTCPConnPool - create new tcp connection pool for tcp calls to a given address (host, port)
func CreateTCPConnPool(options ...Option) *TcpConnPool {

	pool := &TcpConnPool{
		host:            defaultHost,
		port:            defaultPort,
		mu:              sync.Mutex{},
		maxIdleConn:     defaultMaxIdleConnections,
		maxOpenConn:     defaultMaxOpenConnections,
		idleConnections: make(map[string]*tcpConn),
	}

	for _, opt := range options {
		opt(pool)
	}

	go pool.handleConnectionRequest()

	return pool
}

// put - puts the free connection back into the pool if there is any place
func (p *TcpConnPool) put(conn *tcpConn) {

	log.Printf("putting back connection#%s\n", conn.id)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxIdleConn > 0 && p.maxIdleConn > len(p.idleConnections) { // if there is any place in the pool
		//	 we have a place for this connection in the pool
		p.idleConnections[conn.id] = conn
	} else { // if not, then close the connection and decrement the open connections num
		err := conn.conn.Close()
		if err != nil {
			fmt.Printf("error while closing the conn %s\n", err.Error())
		}
		p.numOfOpenConn--
	}

}

// get - gets a tcp connection from the pool or creates a new one if open conns has not reached the max (max open connections)
func (p *TcpConnPool) get() (*tcpConn, error) {

	// Case #1: get a free available connection from the pool
	p.mu.Lock()
	numOfIdleConnections := len(p.idleConnections)
	if numOfIdleConnections > 0 {

		for k, c := range p.idleConnections {

			delete(p.idleConnections, k)
			p.mu.Unlock()
			log.Printf("getting the existing connection from the pool. ID %s\n", c.id)
			return c, nil
		}

	}

	// Case #2: create a new connection
	// 1. if there is no idle connections in the pool
	// 2. and if there is a space to open a new connection provided that numOpenConn < p.maxOpenConn
	// or maxOpenConn == 0 in which case there is no max open conn limit (no limit to open a new connection)
	if p.maxOpenConn <= 0 || p.numOfOpenConn < p.maxOpenConn {
		p.numOfOpenConn++
		p.mu.Unlock()

		//	Open a new connection
		c, err := p.openNewTCPConnection()
		if err != nil { // rollback
			p.mu.Lock()
			p.numOfOpenConn--
			p.mu.Unlock()
			return nil, err
		}

		log.Printf("creating a new connection. ID %s\n", c.id)

		return c, nil
	}

	// Case #3: queue the request -> this happens when there is no available connection in the pool (above case #1 assures)
	// and we cannot create a new connection due to the max open conn limit (above case #2 assures)
	// and this below piece of code put a new connection request into the queue so that it will be fulfilled once a new connection is available in the pool
	req := &connRequest{
		connChan: make(chan *tcpConn, 1),
		errChan:  make(chan error, 1),
	}

	log.Println("Max open conn and max pool")

	// queue the request
	// todo so what's gonna happen here? sending a connection request via requestChan which will be handle by handleConnectionRequest() func
	p.requestChan <- req
	p.mu.Unlock()

	// blocks until we get a tcp connection or error from the handleConnectionRequest() func
	// todo if the queued request is fulfilled, we will get a new tcp connection via req.connChan which will be send from handleConnectionRequest() func
	select {
	case c := <-req.connChan:
		log.Printf("got the connection from the request channel ID %s\n", c.id)
		return c, nil
	case err := <-req.errChan:
		return nil, err
	}

	// todo if you are wondering - how should a connection request be fulfilled?, it's better to look at handleConnectionRequest function
}

func (p *TcpConnPool) openNewTCPConnection() (*tcpConn, error) {

	addr := net.JoinHostPort(p.host, p.port)

	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)

	if err != nil {
		return nil, err
	}

	c, err := net.Dial("tcp", tcpAddr.String())

	if err != nil {
		return nil, err
	}

	conn := &tcpConn{
		id:   uuid.NewString(),
		pool: p,
		conn: c,
	}

	return conn, nil
}

func (p *TcpConnPool) handleConnectionRequest() {

	for req := range p.requestChan {

		var (
			requestDone = false
			hasTimedOut = false

			timeoutChan = time.After(time.Second * 3) // timeouts after 3 seconds
		)

		// this below code is similar to our get operation which checks if any connection became available in the pool,
		// or we can create a new connection because some other connection exits (being closed)
		// and this runs until it gets a new connection or timeout 3 seconds
		for {

			if requestDone || hasTimedOut {
				break
			}

			select {
			case <-timeoutChan:
				hasTimedOut = true
				req.errChan <- errors.New("connection request timeout")
			default:
				p.mu.Lock()

				// first - try to obtain a connection from the pool
				// second - try to establish a new connection
				// if both does not work, retry it in the next loop until request timeouts (timeoutChan) or get a new connection
				numIdle := len(p.idleConnections)
				if numIdle > 0 {
					for _, c := range p.idleConnections {
						delete(p.idleConnections, c.id)
						p.mu.Unlock()
						req.connChan <- c  // return the connection via the channel
						requestDone = true // break the loop
						break
					}
				} else if p.maxOpenConn > 0 && p.numOfOpenConn < p.maxOpenConn {
					p.numOfOpenConn++
					p.mu.Unlock()

					conn, err := p.openNewTCPConnection()

					if err != nil {
						p.mu.Lock()
						p.numOfOpenConn--
						p.mu.Unlock()
						req.errChan <- err
					} else {
						req.connChan <- conn
						requestDone = true
					}

				} else {
					p.mu.Unlock()
				}

			}

		}

	}

}

// TCP CONNECTION
type tcpConn struct {
	id   string // connection unique id
	pool *TcpConnPool
	conn net.Conn
}

// Read - reads data from the connection
func (t *tcpConn) Read() ([]byte, error) {

	// reads the first [4 bytes] from the connection to determine the content length
	prefix := make([]byte, prefixSize)

	_, err := io.ReadFull(t.conn, prefix)

	if err != nil {
		return nil, err
	}

	// reads the remaining data based on the size obtained in the first read
	contentSize := binary.BigEndian.Uint32(prefix[:]) // the size of the actual content

	content := make([]byte, contentSize-prefixSize) // the actual content

	_, err = io.ReadFull(t.conn, content)

	if err != nil {
		return nil, err
	}

	// if you get confused please take a look at the helper function - createTCPBuffer(data []byte) []byte
	return content, nil
}

func (t *tcpConn) Write(data []byte) (int, error) {
	buffData := createTCPBuffer(data)
	return t.conn.Write(buffData)
}
