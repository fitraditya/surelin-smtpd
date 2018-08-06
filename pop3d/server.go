package pop3d

import (
  "bufio"
  "fmt"
  "io"
  "net"
  "runtime"
  "strconv"
  "sync"
  "time"

  "github.com/fitraditya/surelin-smtpd/config"
  "github.com/fitraditya/surelin-smtpd/data"
  "github.com/fitraditya/surelin-smtpd/log"
)

var commands = map[string]bool{
  "TOP":      true,
  "USER":     true,
  "PASS":     true,
  "STAT":     true,
  "LIST":     true,
  "UIDL":     true,
  "RETR":     true,
  "DELE":     true,
  "QUIT":     true,
}

type Server struct {
  Store           *data.DataStore
  domain          string
  maxIdleSeconds  int
  listener        net.Listener
  waitgroup       *sync.WaitGroup
  shutdown        bool
  Debug           bool
  DebugPath       string
  sem             chan int
}

// Init a new Server object
func NewPop3Server(cfg config.Pop3Config, ds *data.DataStore) *Server {
  // sem is an active clients channel used for counting clients
  maxClients := make(chan int, cfg.MaxClients)

  return &Server{
    Store:           ds,
    domain:          cfg.Domain,
    maxIdleSeconds:  cfg.MaxIdleSeconds,
    waitgroup:       new(sync.WaitGroup),
    sem:             maxClients,
  }
}

// Main listener loop
func (s *Server) Start() {
  cfg := config.GetPop3Config()
  defer s.Stop()
  addr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%v:%v", cfg.Ip4address, cfg.Ip4port))

  if err != nil {
    log.LogError("Failed to build tcp4 address: %v", err)
    // TODO More graceful early-shutdown procedure
    s.Stop()
    return
  }

  // Start listening for POP3 connections
  log.LogInfo("POP3 listening on TCP4 %v", addr)
  s.listener, err = net.ListenTCP("tcp4", addr)

  if err != nil {
    log.LogError("POP3 failed to start tcp4 listener: %v", err)
    // TODO More graceful early-shutdown procedure
    s.Stop()
    return
  }

  var tempDelay time.Duration
  var clientId int64

  // Handle incoming connections
  for clientId = 1; ; clientId++ {
    if conn, err := s.listener.Accept(); err != nil {
      if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
        // Temporary error, sleep for a bit and try again
        if tempDelay == 0 {
          tempDelay = 5 * time.Millisecond
        } else {
          tempDelay *= 2
        }

        if max := 1 * time.Second; tempDelay > max {
          tempDelay = max
        }

        log.LogError("POP3 accept error: %v; retrying in %v", err, tempDelay)
        time.Sleep(tempDelay)
        continue
      } else {
        if s.shutdown {
          log.LogTrace("POP3 listener shutting down on request")
          return
        }

        // TODO Implement a max error counter before shutdown
        panic(err)
      }
    } else {
      tempDelay = 0
      s.waitgroup.Add(1)
      log.LogInfo("There are now %s serving goroutines", strconv.Itoa(runtime.NumGoroutine()))
      host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
      s.sem <- 1
      go s.handleClient(&Client{
        state:      1,
        server:     s,
        conn:       conn,
        remoteHost: host,
        time:       time.Now().Unix(),
        bufin:      bufio.NewReader(conn),
        bufout:     bufio.NewWriter(conn),
        id:         clientId,
      })
    }
  }
}

// Stop requests the POP3 server closes it's listener
func (s *Server) Stop() {
  log.LogTrace("POP3 shutdown requested, connections will be drained")
  s.shutdown = true
  s.listener.Close()
}

// Drain causes the caller to block until all active POP3 sessions have finished
func (s *Server) Drain() {
  s.waitgroup.Wait()
  log.LogTrace("POP3 connections drained")
}

func (s *Server) closeClient(c *Client) {
  c.bufout.Flush()
  time.Sleep(200 * time.Millisecond)
  c.conn.Close()
  <-s.sem
}

func (s *Server) killClient(c *Client) {
  c.kill_time = time.Now().Unix()
}

func (s *Server) handleClient(c *Client) {
  log.LogInfo("POP3 Connection from %v, starting session <%v>", c.conn.RemoteAddr(), c.id)

  defer func() {
    s.closeClient(c)
    s.waitgroup.Done()
  }()

  c.greet()
  var ret = false

  // This is our command reading loop
  for {
    line, err := c.readLine()

    if err == nil {
      cmd, args := c.parseCmd(line)
      ret = c.handle(cmd, args, line)

      if ret {
        return
      }
    } else {
      // readLine() returned an error
      if err == io.EOF {
        c.logWarn("Got EOF while in state %v", c.state)
        return
      }

      // not an EOF
      c.logWarn("Connection error: %v", err)

      if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
        c.Write("Idle timeout, bye bye")
        return
      }

      c.Write("Connection error, sorry")
      return
    }
  }

  c.logInfo("Closing connection")
}
