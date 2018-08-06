package smtpd

import (
  "fmt"
  "net"
  "net/smtp"
  "strings"

  "github.com/fitraditya/surelin-smtpd/config"
  "github.com/fitraditya/surelin-smtpd/data"
  "github.com/fitraditya/surelin-smtpd/log"
)

var (
  ports = []int{25, 2525, 587}
)

type Mailer struct {
  Config          config.SmtpConfig
  Store           *data.DataStore
  SendMailChan    chan *config.SMTPMessage
  NotifyMailChan  chan interface{}
}

func NewMailer(ds *data.DataStore) *Mailer {
  cfg := config.GetSmtpConfig()
  sendMailChan := make(chan *config.SMTPMessage, 256)
  notifyMailChan := make(chan interface{}, 256)
  return &Mailer{Config: cfg, Store: ds, SendMailChan: sendMailChan, NotifyMailChan: notifyMailChan}
}

func (md *Mailer) Start() {
  // Start some mailer daemon
  for i := 0; i < 3; i++ {
    go md.SendMail(i)
  }
}

func (md *Mailer) SendMail(id int) {
  log.LogTrace("Running Mailer Daemon #<%d>", id)

  for {
    mc := <-md.SendMailChan

    for i := range mc.To {
      if strings.Contains(mc.To[i], md.Config.Domain) {
        md.Store.SaveMailChan <- mc
      } else {
        if !strings.Contains(mc.To[i], "@") {
          log.LogError("Invalid recipient address: <%s>", mc.To[i])
          return
        }

        host := strings.Split(mc.To[i], "@")[1]
        addr, err := net.LookupMX(host)

        if err != nil {
          log.LogError("Cannot not lookup host: <%s>", addr)
          return
        }

        c, err := newClient(addr, ports)

        if err != nil {
          log.LogError("Cannot not create SMTP client")
          return
        }

        err = send(c, mc.From, mc.To[i], mc.Data)

        if err != nil {
          log.LogError("Cannot not send message")
          return
        }
      }
    }
  }
}

func newClient(mx []*net.MX, ports []int) (*smtp.Client, error) {
  for i := range mx {
    for j := range ports {
      server := strings.TrimSuffix(mx[i].Host, ".")
      hostPort := fmt.Sprintf("%s:%d", server, ports[j])
      client, err := smtp.Dial(hostPort)

      if err != nil {
        if j == len(ports)-1 {
          return nil, fmt.Errorf("Couldn't connect to servers %v on port %d", mx, ports[j])
        }

        continue
      }

      return client, nil
    }
  }

  return nil, fmt.Errorf("Couldn't connect to servers %v on any common port", mx)
}

func send(c *smtp.Client, from string, to string, msg string) error {
  if err := c.Mail(from); err != nil {
    return err
  }

  if err := c.Rcpt(to); err != nil {
    return err
  }

  m, err := c.Data()

  if err != nil {
    return err
  }

  /*
  if m.Subject != "" {
    _, err = msg.Write([]byte("Subject: " + m.Subject + "\r\n"))

    if err != nil {
      return err
    }
  }

  if m.From != "" {
    _, err = msg.Write([]byte("From: <" + m.From + ">\r\n"))

    if err != nil {
      return err
    }
  }

  if m.To != "" {
    _, err = msg.Write([]byte("To: <" + m.To + ">\r\n"))

    if err != nil {
      return err
    }
  }
  */

  _, err = fmt.Fprint(m, msg)

  if err != nil {
    return err
  }

  err = m.Close()

  if err != nil {
    return err
  }

  err = c.Quit()

  if err != nil {
    return err
  }

  return nil
}
