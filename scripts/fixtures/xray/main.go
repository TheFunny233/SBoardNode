package main

import (
	"log"
	"net"
)

func main() {
	listener, err := net.Listen("tcp", "0.0.0.0:18443")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	for {
		connection, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		_ = connection.Close()
	}
}
