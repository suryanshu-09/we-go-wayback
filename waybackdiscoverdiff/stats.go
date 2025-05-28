package waybackdiscoverdiff

import "github.com/smira/go-statsd"

var ClientStatsd = statsd.NewClient("localhost:8125")

// ClientStatsd.Close()
