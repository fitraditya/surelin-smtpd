Surelin-SMTPD
=========================================================

A Lightweight High Performance SMTP written in Go, made for receiving 
large volumes of mail, parse and store in mongodb. The purpose of this daemon is 
to grab the email, save it to the database and disconnect as quickly as possible.

This server does not attempt to check for spam or do any sender 
verification. These steps should be performed by other programs.
The server does NOT send any email including bounces. This should
be performed by a separate program.

The most alluring aspect of Go are the Goroutines! It makes concurrent programming
easy, clean and fun! Go programs can also take advantage of all your machine's multiple 
cores without much effort that you would otherwise need with forking or managing your
event loop callbacks, etc. Golang solves the C10K problem in a very interesting way
 http://en.wikipedia.org/wiki/C10k_problem

Once compiled, Surelin-SMTPD does not have an external dependencies (HTTP and SMTP are all built in).

Protocol Supported
=========================================================

* ESMTP (RFC5321)
* SMTP AUTH (RFC4954) and PIPELINING (RFC2920)
* POP3 (RFC1939)

Features
=========================================================
* Built-in SMTP server
* Built-in POP3 server
* Built-in MTA
* No installation required
* Lightweight and portable
* MongoDB storage for message persistence

To Do
=========================================================
[ ] Support STARTTSL and SSL/TLS
[ ] Built-in IMAP server
[ ] Built-in web based mail client
[ ] Admin interface (domain and user management)

Building from Source
=========================================================

You will need a functioning [Go installation][Golang] for this to work.

Grab the Smtpd source code and compile the daemon:

    go get -v github.com/fitraditya/surelin-smtpd

Edit etc/smtpd.conf and tailor to your environment. It should work on most
Unix and OS X machines as is. Launch the daemon:

    $GOPATH/bin/surelin-smtpd -config=$GOPATH/src/github.com/fitraditya/surelin-smtpd/etc/smtpd.conf

By default the SMTP server will be listening on localhost port 25000 and
the web interface will be available at [localhost:10025](http://localhost:10025/).

Credits
=========================================================

This project is based on [smtpd](https://github.com/gleez/smtpd).

Licence
=========================================================

Released under MIT license, see [LICENSE](license) for details.
