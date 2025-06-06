package waybackdiscoverdiff

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/crossedbot/simplesurt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	s "github.com/suryanshu-09/simhash"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/net/html"
)

// Process HTML document and get key features as text. Steps:
// kill all script and style elements
// get lowercase text
// remove all punctuation
// break into lines and remove leading and trailing space on each
// break multi-headlines into a line each
// drop blank lines
// return a dict with features and their weights

func ExtractHTMLFeatures(htmlContent string) map[string]int {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	features := make(map[string]int)
	var featureData []string

	for n := range doc.Descendants() {
		switch n.Type {
		case html.ElementNode:
			if n.Data == "script" || n.Data == "style" {
				// fmt.Println("script or style tags")
				continue
			}
		case html.CommentNode:
			// fmt.Println("CommentNode")
			continue
		case html.TextNode:
			lowercaseData := strings.Fields(strings.ToLower(n.Data))

			var builder strings.Builder
			for _, str := range lowercaseData {
				for i, r := range str {
					if r == '/' {
						continue
					}

					if r == '\\' && str[i+1] == 'x' {
						builder.WriteRune(r)
						continue
					}

					if unicode.IsControl(r) || unicode.IsPunct(r) {
						builder.WriteRune(' ')
						continue
					}
					builder.WriteRune(r)
				}
				builder.WriteRune(' ')
			}
			unquoted, err := strconv.Unquote(`"` + builder.String() + `"`)
			if err == nil {
				cleanData := strings.Fields(unquoted)
				featureData = append(featureData, cleanData...)
			} else {
				cleanData := strings.Fields(builder.String())
				featureData = append(featureData, cleanData...)
			}
		}
	}

	for _, feat := range featureData {
		features[feat]++
	}

	return features
}

type Simhash struct {
	Hash      *big.Int
	BitLength int
}

// """Calculate simhash for features in a dict. `features_dict` contains data
// like {'text': weight}
// """
func CalculateSimhash(features map[string]int, bitLength int, hashFuncs ...s.HashFunc) (simhash Simhash) {
	var hash *s.Simhash
	if len(hashFuncs) > 0 && hashFuncs[0] != nil {
		hash = s.NewSimhash(features, s.WithF(bitLength), s.WithHashFunc(hashFuncs[0]))
	} else {
		hash = s.NewSimhash(features, s.WithF(bitLength))
	}

	return Simhash{
		Hash:      hash.Value,
		BitLength: bitLength,
	}
}

func PackSimhashToBytesBig(simhash *Simhash, bitLength int) []byte {
	var sizeInBytes int
	if bitLength == 0 {
		sizeInBytes = (simhash.BitLength + 7) / 8
	} else {
		sizeInBytes = bitLength / 8
	}
	return simhash.Hash.FillBytes(make([]byte, sizeInBytes))
}

func PackSimhashToBytes(simhash *Simhash, bitLength int) []byte {
	b := PackSimhashToBytesBig(simhash, bitLength)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return b
}

// """Required by Simhash
// """
func CustomHashFunc(data []byte) []byte {
	hash := blake2b.Sum512(data)
	return hash[:]
}

//---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

// # If a simhash calculation for a URL & year does more than
// # `max_download_errors`, stop it to avoid pointless requests. The captures
// # are not text/html or there is a problem with the WBM.

var (
	maxDownloadErrors  = 10
	maxCaptureDownload = 1000000
)

type CFGSimhash struct {
	Size        int
	ExpireAfter int
}

type Snapshots struct {
	NumberPerYear int
	NumberPerPage int
}

type CFG struct {
	Simhash      CFGSimhash
	Redis        *redis.Options
	Threads      int
	Snapshots    Snapshots
	CdxAuthToken string
}

type Discover struct {
	simhashSize     int
	simhashExpire   int
	http            *http.Client
	request         map[string]string
	redis           *redis.Client
	maxWorkers      int
	snapshotsNumber int
	downloadErrors  int
	log             *slog.Logger
	seen            map[string]string
	Url             string
	ctx             context.Context
	jobId           string
	state           map[string]string
}

func NewDiscover(cfg CFG) *Discover {
	if cfg.Simhash.Size > 512 {
		panic("do not support simhash longer than 512")
	}

	cdxAuthToken := cfg.CdxAuthToken

	requestHeaders := map[string]string{
		"User-Agent":      "wayback-discover-diff",
		"Accept-Encoding": "gzip,deflate",
		"Connection":      "keep-alive",
	}
	if cdxAuthToken != "" {
		requestHeaders["cookie"] = fmt.Sprintf("cdx_auth_token=%s", cdxAuthToken)
	}

	httpTransport := &http.Transport{
		MaxIdleConns:    50,
		MaxConnsPerHost: 50,
		IdleConnTimeout: 20 * time.Second,
	}

	d := &Discover{
		simhashSize:   cfg.Simhash.Size,
		simhashExpire: cfg.Simhash.ExpireAfter,
		http: &http.Client{
			Timeout:   20 * time.Second,
			Transport: httpTransport,
		},
		request:         requestHeaders,
		redis:           redis.NewClient(cfg.Redis),
		maxWorkers:      cfg.Threads,
		snapshotsNumber: cfg.Snapshots.NumberPerYear,
		downloadErrors:  0,
		log:             slog.New(slog.NewTextHandler(os.Stdout, nil)),
		seen:            make(map[string]string),
	}
	return d
}

// """Download capture data from the WBM and update job status. Return
// data only when its text or html. On download error, increment download_errors
// which will stop the task after 10 errors. Fetch data up to a limit
// to avoid getting too much (which is unnecessary) and have a consistent
// operation time.
// """
func (d *Discover) DownloadCapture(ts string) []byte {
	StatsdInc("download-capture", 1)
	d.log.Info("fetching capture", "ts", ts, "url", d.Url)

	captureURL := fmt.Sprintf("https://web.archive.org/web/%sid_/%s", ts, d.Url)
	req, err := http.NewRequest("GET", captureURL, nil)
	if err != nil {
		d.downloadErrors++
		d.log.Error("cannot create request", "ts", ts, "url", d.Url, "err", err)
		return nil
	}

	for key, value := range d.request {
		req.Header.Set(key, value)
	}

	resp, err := d.http.Do(req)
	if err != nil {
		d.downloadErrors++
		StatsdInc("download-error", 1)
		d.log.Error("cannot fetch capture", "ts", ts, "url", d.Url, "err", err)
		return nil
	}
	defer resp.Body.Close()

	limitedReader := io.LimitReader(resp.Body, int64(maxCaptureDownload))
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		d.downloadErrors++
		d.log.Error("cannot read response body", "ts", ts, "url", d.Url, "err", err)
		return nil
	}

	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ctype, "text") || strings.Contains(ctype, "html") {
		return data
	}

	return nil
}

// """Used for performance testing only.
// """

func (d *Discover) StartProfiling(snapshot, index string) {
	f, err := os.Create("profile.prof")
	if err != nil {
		d.log.Error("failed to create profile file", "error", err)
		return
	}
	defer f.Close()

	// Start CPU profiling
	if err := pprof.StartCPUProfile(f); err != nil {
		d.log.Error("could not start CPU profile", "error", err)
		return
	}
	defer pprof.StopCPUProfile()

	// Run the actual function
	capture := fmt.Sprintf("%s %s", snapshot, index)
	_ = d.GetCalc(capture)
}

// """if a capture with an equal digest has been already processed,
// return cached simhash and avoid redownloading and processing. Else,
// download capture, extract HTML features and calculate simhash.
// If there are already too many download failures, return None without
// any processing to avoid pointless requests.
// Return None if any problem occurs (e.g. HTTP error or cannot calculate)
// """
type TimestampSimhash struct {
	Timestamp string
	Simhash   string
}

func (d *Discover) GetCalc(capture string) *TimestampSimhash {
	captureArr := strings.Split(capture, " ")
	if len(captureArr) != 2 {
		d.log.Error("invalid capture format", "capture", capture)
		return nil
	}
	timestamp := captureArr[0]
	digest := captureArr[1]

	simhashEnc, seen := d.seen[digest]
	if seen {
		d.log.Info("already seen", "digest", digest)
		return &TimestampSimhash{timestamp, simhashEnc}
	}

	if d.downloadErrors >= maxDownloadErrors {
		StatsdInc("multiple-consecutive-errors", 1)
		d.log.Error("consecutive download errors", "downloadErrors", d.downloadErrors, "url", d.Url)
		return nil
	}

	responseData := d.DownloadCapture(timestamp)
	if len(responseData) > 0 {
		data := ExtractHTMLFeatures(string(responseData))
		if len(data) > 0 {
			StatsdInc("calculate-simhash", 1)
			d.log.Info("calculating simhash")
			simhash := s.NewSimhash(data, s.WithF(d.simhashSize), s.WithHashFunc(CustomHashFunc))

			simhashBytes := PackSimhashToBytes(&Simhash{Hash: simhash.Value, BitLength: simhash.F}, d.simhashSize)
			simhashEnc := base64.StdEncoding.EncodeToString(simhashBytes)

			d.seen[digest] = simhashEnc
			return &TimestampSimhash{timestamp, simhashEnc}
		}
	}

	return nil
}

func (d *Discover) updateState(state map[string]string) {
	d.state = state
}

func (d *Discover) GetState() map[string]string {
	return d.state
}

func (d *Discover) Run(URL, year string, created time.Time) map[string]any {
	timeStarted := time.Now()
	d.jobId = uuid.New().String()

	pUrl, _ := url.Parse(URL)
	d.Url = string(pUrl.String())
	d.downloadErrors = 0
	d.log.Info("Start calculating simhashes")
	wait := time.Since(created).Milliseconds()
	StatsdTiming("task-wait", int(wait))

	if d.Url == "" {
		d.log.Error("did not give url parameter")
		return map[string]any{"status": "error", "info": "URL is required."}
	}
	if year == "" {
		d.log.Error("did not give year parameter")
		return map[string]any{"status": "error", "info": "Year is required."}
	}

	d.updateState(map[string]string{"PENDING": fmt.Sprintf("Fetching %s captures for year %s", URL, year)})

	resp := d.FetchCDX(URL, year)
	if resp["status"] == "error" {
		return resp
	}

	captures := resp["captures"].([]string)
	d.seen = make(map[string]string)
	finalResults := make(map[string]string)

	numWorkers := d.maxWorkers
	captureChan := make(chan string)
	resultChan := make(chan TimestampSimhash)
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for capture := range captureChan {
				if result := d.GetCalc(capture); result != nil {
					resultChan <- *result
				}
			}
		}()
	}

	go func() {
		for _, capture := range captures {
			captureChan <- capture
		}
		close(captureChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	count := 0
	total := len(captures)
	for res := range resultChan {
		finalResults[res.Timestamp] = res.Simhash
		count++
		if count%10 == 0 {
			d.updateState(map[string]string{"PENDING": fmt.Sprintf("Processed %d out of %d captures", count, total)})
		}
	}

	finLen := strconv.Itoa(len(finalResults))
	d.log.Info("final results", "final results length", finLen, "url", d.Url, "year", year)

	if len(finalResults) > 0 {
		urlkey, _ := simplesurt.Format(d.Url)
		err := d.redis.HMSet(d.ctx, urlkey, finalResults).Err()
		if err != nil {
			d.log.Error("cannot write simhashes to Redis", "url", d.Url, "error", err)
		}
		d.redis.Expire(d.ctx, urlkey, time.Duration(d.simhashExpire)*time.Second)
	}

	duration := time.Since(timeStarted).Milliseconds()
	StatsdTiming("task-duration", int(duration))
	d.log.Info("Simhash calculation finished in", "duration", duration)
	return map[string]any{"duration": fmt.Sprintf("%.2f", float64(duration))}
}

// """Make a CDX query for timestamp and digest for a specific year.
// """
func (d *Discover) FetchCDX(URL, year string) map[string]any {
	d.log.Info("fetching CDX", "url", URL, "year", year)

	params := url.Values{}
	params.Set("url", URL)
	params.Set("from", year)
	params.Set("to", year)
	params.Set("statuscode", "200")
	params.Set("fl", "timestamp,digest")
	params.Set("collapse", "timestamp:9")
	if d.snapshotsNumber != -1 {
		params.Set("limit", strconv.Itoa(d.snapshotsNumber))
	}

	reqUrl := fmt.Sprintf("https://web.archive.org/web/timemap?%s", params.Encode())
	resp, err := d.http.Get(reqUrl)
	if err != nil {
		d.log.Error("HTTP request failed", "error", err)
		return map[string]any{"status": "error", "info": err.Error()}
	}
	defer resp.Body.Close()

	d.log.Info("finished fetching timestamps", "url", URL, "year", year)

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"status": "error", "info": fmt.Sprintf("unexpected status code: %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"status": "error", "info": err.Error()}
	}

	if len(body) == 0 {
		d.log.Info("no captures found", "url", URL, "year", year)
		urlkey, _ := simplesurt.Format(URL)
		_ = d.redis.HSet(d.ctx, urlkey, year, -1).Err()
		_ = d.redis.Expire(d.ctx, urlkey, time.Duration(d.simhashExpire)*time.Second).Err()

		return map[string]any{"status": "error", "info": fmt.Sprintf("No captures of %s for year %s", URL, year)}
	}

	captures := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(captures) > 0 {
		return map[string]any{"status": "success", "captures": captures}
	}

	return map[string]any{"status": "error", "info": fmt.Sprintf("No captures of %s for year %s", URL, year)}
}
