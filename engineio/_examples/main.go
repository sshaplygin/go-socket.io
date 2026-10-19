package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	eio := engineio.NewServer()
	httpServer := &http.Server{Handler: eio}
	var connections sync.WaitGroup
	results := make(chan error, 2)
	remaining := 0
	defer func() {
		err = errors.Join(err, eio.Close())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, httpServer.Shutdown(shutdownCtx), httpServer.Close())
		for ; remaining > 0; remaining-- {
			err = errors.Join(err, <-results)
		}
		// The accept loop has stopped adding connections before Wait.
		connections.Wait()
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	fmt.Printf("Listening on http://%s\n", listener.Addr())
	remaining = 2
	go func() {
		serveErr := httpServer.Serve(listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		results <- serveErr
	}()
	go func() {
		for {
			conn, acceptErr := eio.Accept()
			if acceptErr != nil {
				if errors.Is(acceptErr, io.EOF) {
					acceptErr = nil
				}
				results <- acceptErr
				return
			}
			connections.Add(1)
			go func() {
				defer connections.Done()
				if echoErr := echo(conn); echoErr != nil && !errors.Is(echoErr, io.EOF) {
					log.Printf("connection %s: %v", conn.ID(), echoErr)
				}
			}()
		}
	}()
	select {
	case err = <-results:
		remaining--
	case <-ctx.Done():
	}
	return err
}

// A failed peer ends only its own echo loop. Every acquired reader/writer is closed.
func echo(conn engineio.Conn) (err error) {
	defer func() { err = errors.Join(err, conn.Close()) }()
	fmt.Println(conn.ID(), conn.RemoteAddr(), "->", conn.LocalAddr(), "with", conn.RemoteHeader())
	for {
		typ, reader, readErr := conn.NextReader()
		if readErr != nil {
			return readErr
		}
		writer, writeErr := conn.NextWriter(typ)
		if writeErr != nil {
			return errors.Join(writeErr, reader.Close())
		}
		_, copyErr := io.Copy(writer, reader)
		writeErr = writer.Close()
		readErr = reader.Close()
		if err := errors.Join(copyErr, writeErr, readErr); err != nil {
			return err
		}
	}
}
