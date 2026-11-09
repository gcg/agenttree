package ctl

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Message struct {
	Kind    string `json:"kind"`
	Action  string `json:"action,omitempty"`
	Feature string `json:"feature,omitempty"`
	Event   string `json:"event,omitempty"`
}

type Server struct {
	listener net.Listener
	messages chan Message
	quit     chan struct{}
	once     sync.Once
}

func SocketPath(gitCommonDir string) string {
	return filepath.Join(gitCommonDir, "agenttree", "ctl.sock")
}

func StartServer(sockPath string) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	_ = os.Remove(sockPath)

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %q: %w", sockPath, err)
	}

	srv := &Server{
		listener: l,
		messages: make(chan Message, 64),
		quit:     make(chan struct{}),
	}

	go srv.acceptLoop()

	return srv, nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				return
			}
		}

		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(2 * time.Second))
			var msg Message
			if err := json.NewDecoder(c).Decode(&msg); err == nil {
				select {
				case s.messages <- msg:
				case <-s.quit:
				}
			}
		}(conn)
	}
}

func (s *Server) Messages() <-chan Message {
	return s.messages
}

func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.quit)
		err = s.listener.Close()
	})
	return err
}

func Send(sockPath string, msg Message) error {
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial unix socket %q: %w", sockPath, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		return fmt.Errorf("encode ctl message: %w", err)
	}
	return nil
}
