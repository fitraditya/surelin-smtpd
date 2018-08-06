package pop3d

import (
  "bufio"
  "fmt"
  "net"
  "strconv"
  "strings"
  "time"
  "errors"

  "github.com/fitraditya/surelin-smtpd/log"
)

const (
  UNAUTHORIZED State = iota
  TRANSACTION
  UPDATE
)

type State int

type Client struct {
  server      *Server
  state       State
  remoteHost  string
  time        int64
  conn        net.Conn
  bufin       *bufio.Reader
  bufout      *bufio.Writer
  kill_time   int64
  id          int64
  email       string
}

// Commands are dispatched to the appropriate handlers
func (c *Client) handle(cmd string, args []string, line string) (ret bool) {
  c.logTrace("In state %d, got command '%s', args '%s'", c.state, cmd, args)
  c.logTrace(">" + cmd + "<")
  arg, _ := c.parseArgs(args, 0)
  c.logTrace("line>" + arg + "<")
  c.logTrace(line)

  if cmd == "USER" && c.state == UNAUTHORIZED {
    c.email, _ = c.parseArgs(args, 0)

    if c.server.Store.CheckUserExists(c.email) {
      c.Write("+OK name is a valid mailbox")
      c.logTrace(">+OK name is a valid mailbox")
    } else {
      c.Write("-ERR never heard of mailbox name " + c.email)
      c.logTrace(">-ERR never heard of mailbox name " + c.email)
    }

    return false
  } else if cmd == "PASS" && c.state == UNAUTHORIZED {
    pass, _ := c.parseArgs(args, 0)

    if c.server.Store.LoginUser(c.email, pass) {
      c.Write("+OK mailbox ready")
      c.logTrace(">+OK mailbox ready")
      c.enterState(TRANSACTION)
    } else {
      c.Write("-ERR invalid password")
      c.logTrace(">-ERR invalid password")
    }

    return false
  } else if cmd == "STAT" && c.state == TRANSACTION {
    nr_messages, size_messages := c.server.Store.StatMails(c.email)
    c.Write("+OK " + strconv.Itoa(nr_messages) + " " + strconv.Itoa(size_messages))
    c.logTrace(">+OK " + strconv.Itoa(nr_messages) + " " + strconv.Itoa(size_messages))
  } else if cmd == "LIST" && c.state == TRANSACTION {
    nr, tot_size, Message_head := c.server.Store.ListMails(c.email)
    c.Write("+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")
    c.logTrace(">+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")

    // Print all messages
    for _, val := range Message_head {
      c.Write(strconv.Itoa(val.Id) + " " + strconv.Itoa(val.Size))
    }

    // Ending
    c.Write(".")
    return false
  } else if cmd == "UIDL" && c.state == TRANSACTION {
    nr, tot_size, Message_head := c.server.Store.ListMails(c.email)
    c.Write("+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")
    c.logTrace(">+OK " + strconv.Itoa(nr) + " messages (" + strconv.Itoa(tot_size) + " octets)")

    // Print all messages
    for _, val := range Message_head {
      c.Write(strconv.Itoa(val.Id) + " " + val.Uid)
    }

    // Ending
    c.Write(".")
    return false
  } else if cmd == "RETR" && c.state == TRANSACTION  {
    id, _ := c.parseArgs(args, 0)
    i, _ := strconv.Atoi(id)
    // Retreive one message but don't delete it from the server
    message, size := c.server.Store.GetMail(c.email, i)
    c.Write("+OK " + strconv.Itoa(size) + " octets")
    c.Write(message.Content.Body)
    // Ending
    c.Write(".")
    return false
  } else if cmd == "DELE" && c.state == TRANSACTION  {
    //id, _ := c.parseArgs(args, 0)
    //i, _ := strconv.Atoi(id)
    c.Write("+OK message deleted")
    c.logTrace(">+OK message deleted")
    return false
  } else if cmd == "TOP" && c.state == TRANSACTION {
    arg, _ := c.parseArgs(args, 0)
    nr, _ := strconv.Atoi(arg)
    headers := c.server.Store.TopMail(c.email, nr)
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

func (c *Client) enterState(state State) {
  c.state = state
  c.logInfo("Entering state %v", state)
}

func (c *Client) greet() {
  c.Write(fmt.Sprintf("+OK %v Surelin POP3 # %s (%s) %s", c.server.domain, strconv.FormatInt(c.id, 10), strconv.Itoa(len(c.server.sem)), time.Now().Format(time.RFC1123Z)))
  c.enterState(UNAUTHORIZED)
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
  log.LogWarn("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}

func (c *Client) logError(msg string, args ...interface{}) {
  // Update metrics
  log.LogError("POP3[%v]<%v> %v", c.remoteHost, c.id, fmt.Sprintf(msg, args...))
}
