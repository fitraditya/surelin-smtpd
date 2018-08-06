package smtpd

import (
  "bufio"
  "crypto/tls"
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
  "HELO":     true,
  "EHLO":     true,
  "MAIL":     true,
  "RCPT":     true,
  "DATA":     true,
  "RSET":     true,
  "SEND":     true,
  "SOML":     true,
  "SAML":     true,
  "VRFY":     true,
  "EXPN":     true,
  "HELP":     true,
  "NOOP":     true,
  "QUIT":     true,
  "TURN":     true,
  "AUTH":     true,
  "STARTTLS": true,
}

type Server struct {
  Store           *data.DataStore
  Mailer          *Mailer
  domain          string
  maxRecips       int
  maxIdleSeconds  int
  maxMessageBytes int
  storeMessages   bool
  listener        net.Listener
  shutdown        bool
  waitgroup       *sync.WaitGroup
  timeout         time.Duration
  maxClients      int
  TLSConfig       *tls.Config
  ForceTLS        bool
  Debug           bool
  DebugPath       string
  sem             chan int
  SpamRegex       string
}

// Init a new Server object
func NewSmtpServer(cfg config.SmtpConfig, ds *data.DataStore, md *Mailer) *Server {
  // sem is an active clients channel used for counting clients
  maxClients := make(chan int, cfg.MaxClients)

  return &Server{
    Store:            ds,
    Mailer:           md,
    domain:           cfg.Domain,
    maxRecips:        cfg.MaxRecipients,
    maxIdleSeconds:   cfg.MaxIdleSeconds,
    maxMessageBytes:  cfg.MaxMessageBytes,
    storeMessages:    cfg.StoreMessages,
    waitgroup:        new(sync.WaitGroup),
    Debug:            cfg.Debug,
    DebugPath:        cfg.DebugPath,
    sem:              maxClients,
    SpamRegex:        cfg.SpamRegex,
  }
}

// Main listener loop
func (s *Server) Start() {
  cfg := config.GetSmtpConfig()

  log.LogTrace("Loading the certificate: %s", cfg.PubKey)
  cert, err := tls.LoadX509KeyPair(cfg.PubKey, cfg.PrvKey)

  if err != nil {
    log.LogError("There was a problem with loading the certificate: %s", err)
  } else {
    s.TLSConfig = &tls.Config{
      Certificates: []tls.Certificate{cert},
      ClientAuth:   tls.VerifyClientCertIfGiven,
      ServerName:   cfg.Domain,
    }
  }

  defer s.Stop()
  addr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%v:%v", cfg.Ip4address, cfg.Ip4port))

  if err != nil {
    log.LogError("Failed to build tcp4 address: %v", err)
    // TODO: More graceful early-shutdown procedure
    s.Stop()
    return
  }

  // Start listening for SMTP connections
  log.LogInfo("SMTP listening on TCP4 %v", addr)
  s.listener, err = net.ListenTCP("tcp4", addr)

  if err != nil {
    log.LogError("SMTP failed to start tcp4 listener: %v", err)
    // TODO: More graceful early-shutdown procedure
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

        log.LogError("SMTP accept error: %v; retrying in %v", err, tempDelay)
        time.Sleep(tempDelay)
        continue
      } else {
        if s.shutdown {
          log.LogTrace("SMTP listener shutting down on request")
          return
        }

        // TODO: implement a max error counter before shutdown
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

// Stop requests the SMTP server closes it's listener
func (s *Server) Stop() {
  log.LogTrace("SMTP shutdown requested, connections will be drained")
  s.shutdown = true
  s.listener.Close()
}

// Drain causes the caller to block until all active SMTP sessions have finished
func (s *Server) Drain() {
  s.waitgroup.Wait()
  log.LogTrace("SMTP connections drained")
}

// Handle connected client
func (s *Server) handleClient(c *Client) {
  log.LogInfo("SMTP connection from %v, starting session <%v>", c.conn.RemoteAddr(), c.id)

  defer func() {
    s.closeClient(c)
    s.waitgroup.Done()
  }()

  c.greet()

  // This is our command reading loop
  for {
    if c.state == DATA {
      // Special case, does not use SMTP command format
      c.processData()
      continue
    }

    line, err := c.readLine()

    if err == nil {
      if cmd, arg, ok := c.parseCmd(line); ok {
        c.handle(cmd, arg, line)
      }
    } else {
      // readLine() returned an error
      if err == io.EOF {
        c.logWarn("Got EOF while in state %v", c.state)
        c.enterState(QUIT)
        break
      }

      // Not an EOF
      c.logWarn("Connection error: %v", err)

      if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
        c.Write("221", "Idle timeout, bye bye")
        c.enterState(QUIT)
        break
      }

      c.Write("221", "Connection error, sorry")
      c.enterState(QUIT)
      break
    }

    if c.kill_time > 1 || c.errors > 3 {
      return
    }
  }

  c.logInfo("Closing connection")
}

func (s *Server) killClient(c *Client) {
  c.kill_time = time.Now().Unix()
}

func (s *Server) closeClient(c *Client) {
  c.enterState(QUIT)
  c.bufout.Flush()
  time.Sleep(200 * time.Millisecond)
  c.conn.Close()
  <-s.sem
}
