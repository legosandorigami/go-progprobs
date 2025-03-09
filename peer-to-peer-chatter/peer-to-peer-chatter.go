package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	id                string
	addr              string
	peers             []string             = make([]string, 0)
	mu                *sync.RWMutex        = &sync.RWMutex{}
	errExit           chan bool            = make(chan bool)
	aliveCconnections map[string]*net.Conn = make(map[string]*net.Conn)
)

func handleConnection(ctx context.Context, conn net.Conn, dialer bool) {
	// --- Handshake: exchange IDs ---
	// Send our ID (terminated by newline).
	_, err := conn.Write([]byte(id + "\n"))
	if err != nil {
		log.Println("Error writing id:", err)
		conn.Close()
		return
	}

	// Read the remote ID.
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		log.Println("Error reading remote id:", err)
		conn.Close()
		return
	}

	remoteID := strings.TrimSpace(string(buf[:n]))

	// --- Duplicate connection resolution ---
	// Rule: if our id is lower than remote id and we are the dialer, drop the connection.
	// Conversely, if our id is higher and we are not the dialer (i.e. accepted connection), drop it.
	if id < remoteID && dialer {
		log.Printf("(%s) Dropping outgoing connection to %s\n", id, remoteID)
		conn.Close()
		return
	} else if id > remoteID && !dialer {
		log.Printf("(%s) Dropping incoming connection from %s\n", id, remoteID)
		conn.Close()
		return
	}

	mu.Lock()
	aliveCconnections[remoteID] = &conn
	mu.Unlock()

	defer func() {
		// close the connection and delete from the connectionsAlive map atomically
		mu.Lock()
		conn.Close()
		delete(aliveCconnections, remoteID)
		mu.Unlock()
	}()

	// log.Printf("(%s) Keeping connection with %s\n", id, remoteID)

	dataChan := make(chan []byte)
	errChan := make(chan error)

	go func() {
		readBuf := make([]byte, 256)
		for {
			n, err := conn.Read(readBuf)
			if err != nil {
				errChan <- err
				return
			}
			data := make([]byte, n)
			copy(data, readBuf[:n])
			dataChan <- data
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Printf("(%s) Context cancelled, closing connection with %s\n", id, remoteID)
			return
		case data := <-dataChan:
			processData(string(data))
		case err := <-errChan:
			log.Printf("(%s) Connection closed with %s: %v\n", id, remoteID, err)
			return
		}
	}
}

func connectToPeers(ctx context.Context) {
	for _, peerAddr := range peers {
		for {
			conn, err := net.Dial("tcp", peerAddr)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			} else {
				go handleConnection(ctx, conn, true)
				break
			}
		}
	}
}

func serve(ctx context.Context, listener net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			c, err := listener.Accept()
			if err != nil {
				continue
			}
			go handleConnection(ctx, c, false)
		}
	}
}

func processData(data string) {
	pieces := strings.Split(data, "\t")
	switch pieces[0] {
	case "FWD":
		msg, numStr := pieces[1], strings.TrimSpace(pieces[2])
		num, _ := strconv.Atoi(numStr)
		mu.Lock()
		aliveCs := len(aliveCconnections)
		if num > aliveCs {
			fmt.Println("ERR\tNot Enough Peers.")
		} else {
			done := make(chan bool)
			go writeToPeers(msg, num, done)
			<-done
			fmt.Printf("SUCC\tMessage Sent To %s Peers Successfully.", numStr)
		}
		mu.Unlock()
	case "SUCC":
		fmt.Print(pieces[1])
	case "ERR":
		fmt.Fprint(os.Stderr, pieces[1])
	case "EXIT":
		fmt.Print(pieces[1])
		close(errExit)
	}
}

func writeToPeers(msg string, n int, done chan bool) {
	defer close(done)
	if n == -1 {
		n = rand.Intn(len(aliveCconnections))
		msg = fmt.Sprintf("FWD\t%s\t%d\n", msg, n)
	} else {
		msg = fmt.Sprintf("SUCC\t%s\n", msg)
	}
	i := 0
	for _, conn := range aliveCconnections {
		_, err := io.WriteString(*conn, msg)
		if err != nil {
			log.Printf("unable to send message to the connection %v", *conn)
			log.Println(err)
		}
		if i == n-1 {
			break
		}
	}
}

func sendFWDMessage(ctx context.Context) {
	msg := fmt.Sprintf("Hello from %s", id)
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			done := make(chan bool)
			writeToPeers(msg, -1, done)
			<-done
		}
	}

}

func listenForUserCommands() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		processData(scanner.Text())
	}
}

func main() {
	idFlag := flag.String("id", "", "id of the server, helps in establishing proper connection between the peers")
	addrFlag := flag.String("addr", "", "port number for the server to listen on")
	peersFlag := flag.String("peers", "", "comma seperated port numbers peers are listening on")

	flag.Parse()

	// parsing id
	if *idFlag == "" {
		log.Println("id not provided")
		return
	} else {
		id = *idFlag
	}

	// parsing addr
	if *addrFlag == "" {
		log.Println("addr not provided")
		return
	} else {
		addr = fmt.Sprintf("localhost:%s", *addrFlag)
	}

	// parsing peers
	if *peersFlag == "" {
		log.Println("peers not provided")
		return
	} else {
		peersList := strings.Split(*peersFlag, ",")
		for _, peer := range peersList {
			peers = append(peers, fmt.Sprintf("localhost:%s", strings.Trim(peer, " ")))
		}
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	go serve(ctx, listener)
	connectToPeers(ctx)

	// Listen on stdin for user commands
	go listenForUserCommands()

	// run FWD messenger
	go sendFWDMessage(ctx)

	// for graceful shutdown, closing the channels
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		cancelFunc()
		listener.Close()
		os.Exit(0)
	case <-errExit:
		cancelFunc()
		listener.Close()
		os.Exit(1)
	}
}
