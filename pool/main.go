package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const defaultHOST string = "localhost"
const defaultPORT string = "8090"

func main() {

	//	we need address of the server to which our tcp client connects and sends requests

	tcpPool := CreateTCPConnPool()

	wg := sync.WaitGroup{}

	wg.Add(5)

	for i := 0; i < 5; i++ {

		go func(j int) {

			defer wg.Done()

			if j >= 3 {
				time.Sleep(time.Second * 1)
			}

			conn, err := tcpPool.get()
			if err != nil {
				log.Fatalln(err.Error())
			}

			v := fmt.Sprintf("hello from connection with id %s and message %v", conn.id, j)

			_, err = conn.Write([]byte(v))

			if err != nil {
				fmt.Println("write error")
				// close faulty connections
				conn.conn.Close()
			}

			tcpPool.put(conn)
		}(i)
	}

	wg.Wait()

}
