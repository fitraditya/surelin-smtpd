# INSTALLATION

## Requirements
1. Golang
2. MongoDB

## Build from Source
```
$ mkdir surelin
$ cd surelin
$ export GOPATH=`pwd`
$ go get -v github.com/fitraditya/surelin-smtpd
$ go install github.com/fitraditya/surelin-smtpd
```

## Run Surelin-SMTPD
```
$ $GOPATH/bin/surelin-smtpd -config=$GOPATH/src/github.com/fitraditya/surelin-smtpd/etc/smtpd.conf
```

## SMTP Test Using Telnet
```
$ telnet localhost 25000
```
You will get following response
```
Trying 127.0.0.1...
Connected to localhost.
Escape character is '^]'.
220 localhost Surelin SMTP # 1 (1) Wed, 08 Aug 2018 21:53:18 +0700
```
Press `ctrl + ]` and then type `quit` to quit telnet.

## POP3 Test Using Telnet
```
$ telnet localhost 11000
```
You will get following response
```
Trying 127.0.0.1...
Connected to localhost.
Escape character is '^]'.
+OK localhost Surelin POP3 # 1 (1) Wed, 08 Aug 2018 21:55:32 +0700
```
Press `ctrl + ]` and then type `quit` to quit telnet.
