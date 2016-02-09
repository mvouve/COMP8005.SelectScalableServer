package main

import (
	"container/list"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/goselect"
)

type connectionInfo struct {
	fileDescriptor     int
	timeStamp          time.Time // the time the connection ended
	hostName           string    // the remote host name
	ammountOfData      int       // the ammount of data transfered to/from the host
	numberOfRequests   int       // the total requests sent to the server from this client
	connectionsAtClose int       // the total number of connections being sustained when the connection was closed.
}

type serverInfo struct {
	serverConnection chan int
	connectInfo      chan connectionInfo
	listener         uintptr
}

const newConnectionConst = 1
const finishedConnectionConst = -1

/* Author Marc Vouve
 *
 * Designer Marc Vouve
 *
 * Date: February 6 2016
 *
 * Notes: This is a helper function for when a new connection is detected by the
 *        observer loop
 *
 */
func newConnection(listenFd uintptr) connectionInfo {
	newFileDescriptor, socketAddr, _ := syscall.Accept(int(listenFd))
	switch socketAddr := socketAddr.(type) {
	default:
		fmt.Printf("Unknown socket type %T \n", socketAddr)
	case *syscall.SockaddrInet4:
		hostname := net.IPv4(socketAddr.Addr[0], socketAddr.Addr[1], socketAddr.Addr[2], socketAddr.Addr[3]).String()
		hostname += ":" + string(socketAddr.Port)
	case *syscall.SockaddrInet6:
		hostname := net.IP(socketAddr.Addr[0:16])
	}

	return connectionInfo{fileDescriptor: newFileDescriptor}
}

/* Author: Marc Vouve
 *
 * Designer: Marc Vouve
 *
 * Date: February 6 2016
 *
 * Notes: This function is an "instance" of a server which allows connections in
 *        and echos strings back. After a connection has been closed it will wait
 *        for annother connection
 */
func serverInstance(srvInfo serverInfo) {
	fdSet := new(goselect.FDSet)

	client := make(map[connectionInfo]bool)
	highClient := -1

	// goselect library omits nready, select call using syscall
	nready, err := syscall.Select(highClient, (*syscall.FdSet)(fdSet), nil, nil, nil)
	if err != nil {
		log.Println(err)

		return // block shouldn't be hit under normal conditions.
	}
	for conn, active := range client {
		if !active {
			continue
		}
		if fdSet.IsSet(srvInfo.listener) {
			// add socket to queue

			client[newConnection(srvInfo.listener)] = true
			fdSet.Clear(srvInfo.listener)
		}
		socketfd := conn.fileDescriptor

		if fdSet.IsSet(uintptr(socketfd)) {
			nready--
			read, err := handleData(uintptr(socketfd))
			if err == io.EOF {
				srvInfo.connectInfo <- conn
				delete(client, conn)
			} else if err != nil {
				log.Println(err)
				// also deal with closing
			} else {
				conn.ammountOfData += read
				conn.numberOfRequests++
				fdSet.Clear(uintptr(conn.fileDescriptor))
			}
		}
		if nready <= 0 {
			break
		}
	}
}

/* Author: Marc Vouve
 *
 * Designer: Marc Vouve
 *
 * Date: February 6 2016
 *
 * Returns: connectionInfo information about the connection once the client has
 *          terminated the client.
 *
 * Notes: This function handles actual connections made to the server and tracks
 *        data about the ammount of data and the number of times data is set to
 *        the server.
 *
func connectionInstance(conn net.Conn) connectionInfo {
	connInfo := connectionInfo{hostName: conn.RemoteAddr().String()}

	for {
		err := handleData(conn, &connInfo)
		if err == nil {
			continue
		} else if err == io.EOF {
			break
		}
		log.Println(err)
		break
	}
	connInfo.timeStamp = time.Now()

	return connInfo
}

/**/
func handleData(fd uintptr) (int, error) {
	buf := make([]byte, 1024)
	msg := make([]byte, 0, 1024)
	totalRead := 0
	for {
		read, err := syscall.Read(int(fd), buf[:])
		if err != nil {
			return 0, err
		}
		msg = append(msg, buf[:read]...)
		if buf[read] == '\n' {
			break
		}
		totalRead += read
	}
	syscall.Write(int(fd), msg)

	return totalRead, nil
}

/* Author: Marc Vouve
 *
 * Designer: Marc Vouve
 *
 * Date: February 7 2016
 *
 * Returns: connectionInfo information about the connection once the client has
 *          terminated the client.
 *
 * Notes: This was factored out of the main function.
 */
func observerLoop(srvInfo serverInfo, osSignals chan os.Signal) {
	currentConnections := 0
	connectionsMade := list.New()

	for {
		select {
		case <-srvInfo.serverConnection:
			currentConnections++
		case serverHost := <-srvInfo.connectInfo:
			serverHost.connectionsAtClose = currentConnections
			connectionsMade.PushBack(serverHost)
			currentConnections--
		case <-osSignals:
			generateReport(time.Now().String(), connectionsMade)
			fmt.Println("Total connections made:", connectionsMade.Len())
			os.Exit(1)
		}
	}
}

func newServerInfo() serverInfo {
	srvInfo := serverInfo{
		serverConnection: make(chan int, 10), connectInfo: make(chan connectionInfo)}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.O_NONBLOCK|syscall.SOCK_STREAM, 0)
	if err != nil {
		log.Println(err)
	}
	syscall.SetNonblock(fd, true)
	// TODO: make port vairable
	addr := syscall.SockaddrInet4{Port: 2000}
	copy(addr.Addr[:], net.ParseIP("0.0.0.0").To4())
	syscall.Bind(fd, &addr)
	syscall.Listen(fd, 1000)
	srvInfo.listener = uintptr(fd)

	return srvInfo
}

func (s serverInfo) Close() {
	syscall.Close(int(s.listener))
}

func main() {
	if len(os.Args) < 2 { // validate args
		fmt.Println("Missing args:", os.Args[0], " [PORT]")

		os.Exit(0)
	}

	srvInfo := newServerInfo()
	defer srvInfo.Close()

	// create servers
	for i := 0; i < 8; i++ {
		go serverInstance(srvInfo)
	}

	// when the server is killed it should print statistics need to catch the signal
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, os.Kill)

	observerLoop(srvInfo, osSignals)
}
