package waybackdiscoverdiff

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/smira/go-statsd"
)

var STATSDClient = statsd.NewClient("localhost:8125")

func Configure(host, port string) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	h, err := os.Hostname()
	if err != nil {
		logger.Error("failed to get local hostname", "error", err.Error())
		return
	}
	hostname := strings.Split(h, ".")[0]
	prefix := fmt.Sprintf("wb.changes.%s.", hostname)

	addr := fmt.Sprintf("%s:%s", host, port)
	STATSDClient = statsd.NewClient(addr, statsd.MetricPrefix(prefix))
	logger.Info("configured statsd client", "host", host, "port", port, "prefix", prefix)
}

// STATSDClient.close()

func StatsdInc(metric string, count int) {
	if count <= 0 {
		count = 1
	}
	STATSDClient.Incr(metric, int64(count))
}

func StatsdTiming(metric string, dtSec int) {
	STATSDClient.Timing(metric, int64(dtSec*1000))
}

func Timing(metric string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start)
		STATSDClient.Timing(metric, int64(duration.Milliseconds()))
	}
}
