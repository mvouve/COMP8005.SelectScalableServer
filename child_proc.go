/*------------------------------------------------------------------------------
-- DATE:	       February, 2016
--
-- Source File:	 main.go
--
-- REVISIONS: 	(Date and Description)
--
-- DESIGNER:	   Marc Vouve
--
-- PROGRAMMER:	 Marc Vouve
--
--
-- INTERFACE:
--  func main()
--	func manageConnections(srvInfo serverInfo, osSignals chan os.Signal)
--	func newServerInfo() serverInfo
--  func getAddr() syscall.SockaddrInet4
--  func (s serverInfo) Close()
--
-- NOTES: This is the main file for the select Scalable server
------------------------------------------------------------------------------*/

package main

import (
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"syscall"
)

const bufferSize = 1024

/*******************************************************************************
 * Author Marc Vouve
 *
 * Designer Marc Vouve
 *
 * Date: February 6 2016
 *
 * Params: listenFd: The file descriptor of the listening host
 *
 * Return: connectionInfo of the new connection made
 *
 * Notes: This is a helper function for when a new connection is detected by the
 *        observer loop
 *
 ******************************************************************************/
func newConnection(listenFd int) (connectionInfo, error) {
	newFileDescriptor, socketAddr, err := syscall.Accept(int(listenFd))
	if err != nil {
		return connectionInfo{}, err
	}

	var hostname string
	switch socketAddr := socketAddr.(type) {
	default:
		return connectionInfo{}, err
	case *syscall.SockaddrInet4:
		hostname = net.IPv4(socketAddr.Addr[0], socketAddr.Addr[1], socketAddr.Addr[2], socketAddr.Addr[3]).String()
		hostname += ":" + strconv.FormatInt(int64(socketAddr.Port), 10)
	case *syscall.SockaddrInet6:
		hostname = net.IP(socketAddr.Addr[0:16]).String()
		hostname += ":" + string(socketAddr.Port)
	}
	return connectionInfo{FileDescriptor: newFileDescriptor, HostName: hostname}, nil
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    read
--
-- DATE:        February 6, 2016
--
-- REVISIONS:	  February 11, 2016 - Modified for select
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func fdSET(p *syscall.FdSet, i int)
--
-- RETURNS:    void
--
-- NOTES:			reimplementation of C macro
------------------------------------------------------------------------------*/
func serverInstance(srvInfo serverInfo) {
	var fdSet, rSet syscall.FdSet
	client := make(map[int]connectionInfo)
	highClient := srvInfo.listener

	fdZERO(&fdSet)
	fdSET(&fdSet, srvInfo.listener)

	for {
		copy(rSet.Bits[:], fdSet.Bits[:])
		_, err := syscall.Select(highClient+1, &rSet, nil, nil, nil)
		if err != nil {
			log.Println("err", err)
			return // block shouldn't be hit under normal conditions. If it does something is really wrong.
		}
		if fdISSET(&rSet, srvInfo.listener) { // new client
			newClient, err := newConnection(srvInfo.listener)
			if err == nil {
				client[newClient.FileDescriptor] = newClient
				fdSET(&fdSet, newClient.FileDescriptor)
				srvInfo.serverConnection <- 1
				if newClient.FileDescriptor > highClient {
					highClient = newClient.FileDescriptor
				}
			}
		}
		for conn := range client {
			socketfd := client[conn].FileDescriptor
			if fdISSET(&rSet, socketfd) { // existing connection
				connect := client[conn]
				err := handleData(&connect)
				client[conn] = connect
				if err != nil {
					if err != io.EOF {
						log.Println(err)
					}
					fdCLEAR(&fdSet, client[conn].FileDescriptor)
					endConnection(srvInfo, client[conn])
					delete(client, conn)
				}
			}
		}
	}
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    endConnection
--
-- DATE:        February 6, 2016
--
-- REVISIONS:
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   endConnection(srvInfo serverInfo, conn connectionInfo)
--   srvInfo:   needed to pass the connection info to the parent process
--      conn:   information about the client, allows socket to be close needs to
--              be passed to parent.
--
-- RETURNS:    void
--
-- NOTES:      socket will be closed when this function returns.
------------------------------------------------------------------------------*/
func endConnection(srvInfo serverInfo, conn connectionInfo) {
	srvInfo.connectInfo <- conn
	syscall.Close(conn.FileDescriptor)
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    handleData
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 12, 2016 - made read function its own function.
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func handleData(conn *connectionInfo) (int, error)
--      conn:   structure for the current connection.
--
-- RETURNS:     error: io.EOF if the client closed the connection. or system error.
--              from internal call.
--
-- NOTES:			handles an incoming request from a client.
------------------------------------------------------------------------------*/
func handleData(conn *connectionInfo) error {
	msg, err := read(conn.FileDescriptor)
	if err != nil {
		return err
	}
	syscall.Write(conn.FileDescriptor, []byte(msg))
	conn.NumberOfRequests++
	conn.AmmountOfData += len(msg)

	return nil
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    read
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 12, 2016 - refactored out of handleData function.
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func read(fd int) (string, error)
--      fd:     the file descriptor being read from.
--
-- RETURNS:     string: the data read from the client.
--
-- NOTES:			reads data from a file descriptor, returns EOF on remote closing
--            the connection.
------------------------------------------------------------------------------*/
func read(fd int) (string, error) {
	buf := make([]byte, bufferSize)
	var msg string
	for {
		n, err := syscall.Read(fd, buf[:])
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", io.EOF
		}
		msg += string(buf[:n])

		if strings.ContainsRune(msg, '\n') {
			break
		}
	}

	return msg, nil
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    fdSET
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 11, 2016 Needed to add this because it doesn't exist in go
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func fdSET(p *syscall.FdSet, i int)
--
-- RETURNS:    void
--
-- NOTES:			reimplementation of C macro
------------------------------------------------------------------------------*/
func fdSET(p *syscall.FdSet, i int) {
	p.Bits[i/64] |= (1 << (uint(i) % 64))
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    fdSET
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 11, 2016 Needed to add this because it doesn't exist in go
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func fdSET(p *syscall.FdSet, i int)
--
-- RETURNS:    void
--
-- NOTES:			reimplementation of C macro
------------------------------------------------------------------------------*/
func fdCLEAR(p *syscall.FdSet, i int) {
	p.Bits[i/64] &^= (1 << (uint(i) % 64))
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    fdSET
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 11, 2016 Needed to add this because it doesn't exist in go
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func fdSET(p *syscall.FdSet, i int)
--
-- RETURNS:    void
--
-- NOTES:			reimplementation of C macro
------------------------------------------------------------------------------*/
func fdISSET(p *syscall.FdSet, i int) bool {
	return (p.Bits[i/64] & (1 << (uint(i) % 64))) != 0
}

/*-----------------------------------------------------------------------------
-- FUNCTION:    fdSET
--
-- DATE:        February 7, 2016
--
-- REVISIONS:	  February 11, 2016 Needed to add this because it doesn't exist in go
--
-- DESIGNER:		Marc Vouve
--
-- PROGRAMMER:	Marc Vouve
--
-- INTERFACE:   func fdSET(p *syscall.FdSet, i int)
--
-- RETURNS:    void
--
-- NOTES:			reimplementation of C macro
------------------------------------------------------------------------------*/
func fdZERO(p *syscall.FdSet) {
	for i := range p.Bits {
		p.Bits[i] = 0
	}
}
