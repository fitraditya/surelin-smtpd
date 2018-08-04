package pop3d

import (
  "bufio"
  "fmt"
  "io"
  "net"
  "runtime"
  "strconv"
  "strings"
  "sync"
  "time"
  "errors"

  "github.com/fitraditya/surelin-smtpd/config"
  "github.com/fitraditya/surelin-smtpd/data"
  "github.com/fitraditya/surelin-smtpd/log"
)

const (
  STATE_UNAUTHORIZED = 1
  STATE_TRANSACTION = 2
  STATE_UPDATE = 3
)

type State int

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

// Real server code starts here
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

type Client struct {
  server      *Server
  state       State
  remoteHost  string
  time        int64
  conn         net.Conn
  bufin       *bufio.Reader
  bufout      *bufio.Writer
  kill_time   int64
  id          int64
  tmp_client  string
}

// Init a new Client object
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
    //panic(err)
    s.Stop()
    return
  }

  // Start listening for POP3 connections
  log.LogInfo("POP3 listening on TCP4 %v", addr)
  s.listener, err = net.ListenTCP("tcp4", addr)

  if err != nil {
    log.LogError("POP3 failed to start tcp4 listener: %v", err)
    // TODO More graceful early-shutdown procedure
    //panic(err)
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

        // TODO Implement a max error counter before shutdown?
        // or maybe attempt to restart POP3d
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

// Commands are dispatched to the appropriate handlers.
func (c *Client) handle(cmd string, args []string, line string) (ret bool) {
  c.logTrace("In state %d, got command '%s', args '%s'", c.state, cmd, args)

  c.logTrace(">" + cmd + "<")
  arg, _ := c.parseArgs(args, 0)
  c.logTrace("line>" + arg + "<")
  c.logTrace(line)

  if cmd == "USER" && c.state == STATE_UNAUTHORIZED {
    c.tmp_client, _ = c.parseArgs(args, 0)

    if c.server.Store.CheckUserExists(c.tmp_client) {
      c.Write("+OK name is a valid mailbox")
      c.logTrace(">+OK name is a valid mailbox")
    } else {
      c.Write("-ERR never heard of mailbox name " + c.tmp_client)
      c.logTrace(">-ERR never heard of mailbox name " + c.tmp_client)
    }

    return false
  } else if cmd == "PASS" && c.state == STATE_UNAUTHORIZED {
    pass, _ := c.parseArgs(args, 0)

    if c.server.Store.LoginUser(c.tmp_client, pass) {
      c.Write("+OK mailbox ready")
      c.logTrace(">+OK mailbox ready")
      c.state = 2
    } else {
      c.Write("-ERR invalid password")
      c.logTrace(">-ERR invalid password")
    }

    return false
  } else if cmd == "STAT" && c.state == STATE_TRANSACTION {
    nr_messages, size_messages := c.server.Store.StatMails(c.tmp_client)
    c.Write("+OK " + strconv.Itoa(nr_messages) + " " + strconv.Itoa(size_messages))
    c.logTrace(">+OK " + strconv.Itoa(nr_messages) + " " + strconv.Itoa(size_messages))
  } else if cmd == "LIST" && c.state == STATE_TRANSACTION {
    c.logTrace("List accepted")
    nr, tot_size, Message_head := c.server.Store.ListMails(c.tmp_client)
    c.Write("+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")

    // Print all messages
    for _, val := range Message_head {
      c.Write(strconv.Itoa(val.Id) + " " + strconv.Itoa(val.Size))
    }

    // Ending
    c.Write(".")
    return false
  } else if cmd == "UIDL" && c.state == STATE_TRANSACTION {
    c.logTrace("Uidl accepted")
    nr, tot_size, Message_head := c.server.Store.ListMails(c.tmp_client)
    c.Write("+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")

    // Print all messages
    for _, val := range Message_head {
      c.Write(strconv.Itoa(val.Id) + " " + val.Uid)
    }

    // Ending
    c.Write(".")
    return false
  } else if cmd == "RETR" && c.state == STATE_TRANSACTION  {
    id, _ := c.parseArgs(args, 0)
    i, _ := strconv.Atoi(id)
    // Retreive one message but don't delete it from the server
    message, size := c.server.Store.GetMail(c.tmp_client, i)
    c.Write("+OK " + strconv.Itoa(size) + " octets")
    c.Write(message.Content.Body)
    // Ending
    c.Write(".")
    return false
  } else if cmd == "DELE" && c.state == STATE_TRANSACTION  {
    //id, _ := c.parseArgs(args, 0)
    //i, _ := strconv.Atoi(id)
    // Dummy delete from the server
    c.Write("+OK message deleted")
    return false
  } else if cmd == "TOP" && c.state == STATE_TRANSACTION {
    arg, _ := c.parseArgs(args, 0)
    nr, _ := strconv.Atoi(arg)
    headers := c.server.Store.TopMail(c.tmp_client, nr)
    c.Write("+OK top message follows")
    c.Write(headers + "\r\n\r\n.")
    return false
  } else if cmd == "CAPA" {
    c.Write("TOP")
    c.Write("UIDL")
    // Ending
    c.Write(".")
    return false
  } else if cmd == "QUIT" {
    return true
  } else {
    c.Write("-ERR not implemented")
    return false
  }

  return false
}

func (c *Client) greet() {
  c.Write(fmt.Sprintf("+OK %v Surelin POP3 # %s (%s) %s", c.server.domain, strconv.FormatInt(c.id, 10), strconv.Itoa(len(c.server.sem)), time.Now().Format(time.RFC1123Z)))
  c.state = 1
}

// Calculate the next read or write deadline based on maxIdleSeconds
func (c *Client) nextDeadline() time.Time {
  return time.Now().Add(time.Duration(c.server.maxIdleSeconds) * time.Second)
}

func (c *Client) Write(text string) {
  c.conn.SetDeadline(c.nextDeadline())
  c.logTrace(">> Sent %d bytes: %s >>", len(text), text)
  c.conn.Write([]byte(text + "\r\n"))
  c.bufout.Flush()
}

// Reads a line of input
func (c *Client) readLine() (line string, err error) {
  if err = c.conn.SetReadDeadline(c.nextDeadline()); err != nil {
    return "", err
  }

  line, err = c.bufin.ReadString('\n')

  if err != nil {
    return "", err
  }

  c.logTrace("<< %v <<", strings.TrimRight(line, "\r\n"))
  return line, nil
}

func (c *Client) parseCmd(line string) (cmd string, arg []string) {
  line = strings.Trim(line, "\r \n")
  cm := strings.Split(line, " ")
  return strings.ToUpper((cm[0])[0:4]), cm[1:]
}

func (c *Client) parseArgs(args []string, nr int) (arg string, err error) {
  if nr < len(args) {
    return args[nr], nil
  }

  return "", errors.New("Out of range")
}

// Session specific logging methods
func (c *Client) logTrace(msg string, args ...interface{}) {
  log.LogTrace("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}

func (c *Client) logInfo(msg string, args ...interface{}) {
  log.LogInfo("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}

func (c *Client) logWarn(msg string, args ...interface{}) {
  // Update metrics
  //expWarnsTotal.Add(1)
  log.LogWarn("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}

func (c *Client) logError(msg string, args ...interface{}) {
  // Update metrics
  //expErrorsTotal.Add(1)
  log.LogError("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}
