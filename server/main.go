package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const address = "localhost:8090"

func main() {

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, os.Interrupt)
	go func() {

		<-c
		cancel()

	}()

	go createNewTCPServer(ctx)

	<-ctx.Done()

	time.Sleep(time.Second * 2)
	fmt.Println("graceful shutdown!")

}

func createNewTCPServer(ctx context.Context) {

	listener, err := net.Listen("tcp", address)

	if err != nil {
		log.Fatalf("error happened while listening on the tcp address %s and err: %s\n", address, err.Error())
		return
	}

	defer listener.Close()

	log.Printf("tcp server has start at %s\n", address)

	for {

		netCh := make(chan net.Conn, 1)
		errCh := make(chan error, 1)

		go func(lis net.Listener, netCh chan net.Conn, errCh chan error) {

			conn, err := listener.Accept()

			if err != nil {
				errCh <- err
				return
			}

			netCh <- conn
		}(listener, netCh, errCh)

		select {
		case <-ctx.Done():
			log.Println("shutting down tcp server...")
			return
		case err := <-errCh:
			log.Printf("error while accepting a new connection %s\n", err.Error())
		case conn := <-netCh:
			go handle(conn)
		}
	}

}

func handle(conn net.Conn) {

	defer conn.Close()

	for {

		data, err := Read(conn)

		if err != nil {
			if err != io.EOF {
				fmt.Printf("error occured while reading the data %s\n", err.Error())
			}
			return
		}

		fmt.Println(string(data))
	}

}

// Read - reads data from the connection
func Read(c net.Conn) ([]byte, error) {
	const prefixSize = 4 // 4 bytes - prefix size
	// reads the first [4 bytes] from the connection to determine the content length
	prefix := make([]byte, prefixSize)

	_, err := io.ReadFull(c, prefix)

	if err != nil {
		return nil, err
	}

	// reads the remaining data based on the size obtained in the first read
	contentSize := binary.BigEndian.Uint32(prefix[:]) // the size of the actual content

	content := make([]byte, contentSize-prefixSize) // the actual content

	_, err = io.ReadFull(c, content)

	if err != nil {
		return nil, err
	}

	// if you get confused please take a look at the helper function - createTCPBuffer(data []byte) []byte
	return content, nil
}
