package main

import "log"

const defaultHOST string = "localhost"
const defaultPORT string = "8090"

func main() {

	//	we need address of the server to which our tcp client connects and sends requests

	tcpPool := CreateTCPConnPool()

	messages := []string{"hello", "world"}

	for _, v := range messages {

		conn, err := tcpPool.get()
		if err != nil {
			log.Fatalln(err.Error())
		}

		_, err = conn.Write([]byte(v))

		if err != nil {
			log.Fatalln(err)
		}

		tcpPool.put(conn)
	}

}
