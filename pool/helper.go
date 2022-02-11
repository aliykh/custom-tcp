package main

import "encoding/binary"

const prefixSize = 4 // 4 bytes - prefix size

func createTCPBuffer(data []byte) []byte {

	// create buffer to store the data with its prefix [the first 4 bytes] which indicates actual content length
	buff := make([]byte, prefixSize+len(data))

	// put the size of the actual data into the first 4 bytes
	binary.BigEndian.PutUint32(buff[:prefixSize], uint32(prefixSize+len(data)))

	// copy the actual data into the remaining space after the first 4 bytes
	copy(buff[prefixSize:], data[:])

	return buff
}
