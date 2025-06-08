package waybackdiscoverdiff

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/crossedbot/simplesurt"
	"github.com/go-redis/redis/v8"
	"github.com/hibiken/asynq"
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

					if r == '\\' && i+1 < len(str) && str[i+1] == 'x' {
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
	Year            string
	ctx             context.Context
	jobId           string
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
		redis:           RedisClient,
		maxWorkers:      cfg.Threads,
		snapshotsNumber: cfg.Snapshots.NumberPerYear,
		downloadErrors:  0,
		log:             slog.New(slog.NewTextHandler(os.Stdout, nil)),
		seen:            make(map[string]string, 0),
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

type TimestampSimhash struct {
	Timestamp string
	Simhash   string
}

// """if a capture with an equal digest has been already processed,
// return cached simhash and avoid redownloading and processing. Else,
// download capture, extract HTML features and calculate simhash.
// If there are already too many download failures, return None without
// any processing to avoid pointless requests.
// Return None if any problem occurs (e.g. HTTP error or cannot calculate)
// """
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

func SetJobStatus(ctx context.Context, rdb *redis.Client, jobId, url, year, status string) {
	if jobId == "" {
		log.Printf("Warning: Empty jobId provided to SetJobStatus, status=%s", status)
		return
	}
	value := fmt.Sprintf("%s|%s|%s", status, url, year)
	err := rdb.Set(ctx, jobId, value, time.Hour).Err()
	if err != nil {
		log.Printf("Error setting job status in Redis: %v (jobId: %s, status: %s)", err, jobId, status)
	} else {
		log.Printf("Job status updated successfully: jobId=%s, status=%s", jobId, status)
	}
}

func makeStatusKey(url, year string) string {
	simple, _ := simplesurt.Format(url)

	re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
	urlkey := re.ReplaceAllString(simple, "$1)/")
	return fmt.Sprintf("taskstatus:%s:%s", urlkey, year)
}

type TaskStatus struct {
	TaskType    string `json:"task_type"`
	Status      string `json:"status"`
	Description string `json:"description"`
	ID          string `json:"id"`
}

func SetTaskStatus(ctx context.Context, rdb *redis.Client, taskType, url, year, status, description, jobId string) error {
	if url == "" || year == "" {
		return fmt.Errorf("missing required url or year for task status")
	}

	key := makeStatusKey(url, year)

	val := TaskStatus{
		TaskType:    taskType,
		Status:      status,
		Description: description,
		ID:          jobId,
	}

	jsonVal, err := json.Marshal(val)
	if err != nil {
		return err
	}

	err = rdb.Set(ctx, key, jsonVal, time.Duration(SimhashExpireAfter)*time.Second).Err()
	if err != nil {
		log.Printf("Error setting task status in Redis: %v (key: %s, status: %s)", err, key, status)
		return err
	}
	log.Printf("Task status updated successfully: key=%s, status=%s, jobId=%s", key, status, jobId)
	return nil
}

type HttpResponse struct {
	Status   string `json:"status"`
	Info     any    `json:"info,omitempty"`
	Captures any    `json:"captures,omitempty"`
	Simhash  any    `json:"simhash,omitempty"`
	Message  any    `json:"message,omitempty"`
	JobId    any    `json:"job_id,omitempty"`
	Duration any    `json:"duration,omitempty"`
}

const TypeDiscover = "discover:run"

type DiscoverPayload struct {
	URL     string
	Year    string
	Created time.Time
	JobId   string
}

func NewDiscoverTask(URL, year, JobId string, created time.Time) (*asynq.Task, error) {
	payload, err := json.Marshal(DiscoverPayload{URL: URL, Year: year, Created: created, JobId: JobId})
	if err != nil {
		return nil, err
	}

	task := asynq.NewTask(TypeDiscover, payload)
	return task, nil
}

func (d *Discover) DiscoverTaskHandler(ctx context.Context, t *asynq.Task) error {
	timeStarted := time.Now()
	var payload DiscoverPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		d.log.Error("Failed to unmarshal task payload", "error", err)
		SetJobStatus(ctx, d.redis, d.jobId, "", "", "ERROR")
		return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
	}

	d.log.Info("Task payload unmarshaled successfully", "jobId", payload.JobId, "url", payload.URL, "year", payload.Year)
	d.jobId = payload.JobId
	// Use the provided context instead of the struct's context
	d.ctx = ctx

	pUrl, err := url.ParseRequestURI(payload.URL)
	if err != nil {
		d.log.Error("invalid URL", "url", payload.URL)
		SetJobStatus(ctx, d.redis, d.jobId, "", "", "ERROR")
		return fmt.Errorf("invalid url: %w", asynq.SkipRetry)
	}
	d.Url = pUrl.String()
	d.Year = payload.Year

	d.downloadErrors = 0

	d.log.Info("Job ID", "jobId", d.jobId)
	wait := time.Since(payload.Created).Milliseconds()
	StatsdTiming("task-wait", int(wait))

	if d.Url == "" || d.Year == "" {
		d.log.Error("missing URL or year", "url", d.Url, "year", d.Year)
		SetTaskStatus(ctx, d.redis, TypeDiscover, d.Url, d.Year, "FAILED", "Missing URL or year", d.jobId)
		SetJobStatus(ctx, d.redis, d.jobId, d.Url, d.Year, "FAILED")
		return fmt.Errorf("missing required fields: %w", asynq.SkipRetry)
	}

	d.log.Info("Setting task status to PENDING", "url", d.Url, "year", d.Year, "jobId", d.jobId)
	if err = SetTaskStatus(ctx, d.redis, TypeDiscover, d.Url, d.Year, "PENDING", fmt.Sprintf("Fetching captures for %s", d.Year), d.jobId); err != nil {
		d.log.Error("SetTaskStatus failed", "error", err)
	} else {
		d.log.Info("Task status set to PENDING successfully")
	}

	d.log.Info("Setting job status to PENDING", "jobId", d.jobId)
	SetJobStatus(ctx, d.redis, d.jobId, d.Url, d.Year, "PENDING")
	d.log.Info("Start calculating simhashes")

	resp := d.FetchCDX(d.Url, d.Year)
	if resp.Status == "error" {
		d.log.Error("FetchCDX failed", "url", d.Url, "year", d.Year)

		d.log.Info("Setting task and job status to FAILED", "jobId", d.jobId)
		SetTaskStatus(ctx, d.redis, TypeDiscover, d.Url, d.Year, "FAILED", "FetchCDX failed", d.jobId)
		SetJobStatus(ctx, d.redis, d.jobId, d.Url, d.Year, "FAILED")
		return fmt.Errorf("FetchCDX failed: %v", asynq.SkipRetry)
	}

	captures := resp.Info.([]string)
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

	for res := range resultChan {
		finalResults[res.Timestamp] = res.Simhash
	}

	finLen := strconv.Itoa(len(finalResults))
	d.log.Info("Final results", "count", finLen, "url", d.Url, "year", d.Year)

	if len(finalResults) > 0 {
		simple, _ := simplesurt.Format(d.Url)

		re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
		urlkey := re.ReplaceAllString(simple, "$1)/")
		d.log.Info("Writing simhash results to Redis", "url", d.Url, "urlkey", urlkey, "count", len(finalResults))
		err := d.redis.HMSet(ctx, urlkey, finalResults).Err()
		if err != nil {
			d.log.Error("Failed writing to Redis", "url", d.Url, "error", err)

			d.log.Info("Setting task and job status to FAILED due to Redis write error", "jobId", d.jobId)
			SetTaskStatus(ctx, d.redis, TypeDiscover, d.Url, d.Year, "FAILED", "Redis write failed", d.jobId)
			SetJobStatus(ctx, d.redis, d.jobId, d.Url, d.Year, "FAILED")
			return err
		}
		d.log.Info("Setting expiration for Redis key", "urlkey", urlkey, "seconds", d.simhashExpire)
		if err := d.redis.Expire(ctx, urlkey, time.Duration(d.simhashExpire)*time.Second).Err(); err != nil {
			d.log.Error("Failed setting expiration on Redis key", "urlkey", urlkey, "error", err)
		}
	}

	duration := time.Since(timeStarted).Milliseconds()
	StatsdTiming("task-duration", int(duration))
	d.log.Info("Simhash calculation completed", "duration(ms)", duration)

	d.log.Info("Setting task status to SUCCESS", "url", d.Url, "year", d.Year, "jobId", d.jobId, "duration", duration)
	statusKey := makeStatusKey(d.Url, d.Year)
	d.log.Info("Status key", "key", statusKey)

	if err = SetTaskStatus(ctx, d.redis, TypeDiscover, d.Url, d.Year, "SUCCESS", fmt.Sprintf("Completed in %dms", duration), d.jobId); err != nil {
		d.log.Error("SetTaskStatus failed", "error", err, "statusKey", statusKey)
		return err
	}

	d.log.Info("Task status set to SUCCESS, setting job status", "jobId", d.jobId)
	SetJobStatus(ctx, d.redis, d.jobId, d.Url, d.Year, "SUCCESS")

	// Verify the task status was saved correctly
	key := makeStatusKey(d.Url, d.Year)
	val, getErr := d.redis.Get(ctx, key).Result()
	if getErr != nil {
		d.log.Error("Failed to verify task status", "key", key, "error", getErr)
	} else {
		d.log.Info("Verified task status in Redis", "key", key, "value", val)
	}

	d.log.Info("Task completed successfully", "jobId", d.jobId, "url", d.Url, "year", d.Year)
	return nil
}

// """Make a CDX query for timestamp and digest for a specific year.
// """
func (d *Discover) FetchCDX(URL, year string) HttpResponse {
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
		return HttpResponse{Status: "error", Info: err.Error()}
	}
	defer resp.Body.Close()

	d.log.Info("finished fetching timestamps", "url", URL, "year", year)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HttpResponse{Status: "error", Info: err.Error()}
	}

	if len(body) == 0 {
		d.log.Info("no captures found", "url", URL, "year", year)

		simple, _ := simplesurt.Format(URL)

		re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
		urlkey := re.ReplaceAllString(simple, "$1)/")
		_ = d.redis.HSet(d.ctx, urlkey, year, -1).Err()
		_ = d.redis.Expire(d.ctx, urlkey, time.Duration(d.simhashExpire)*time.Second).Err()

		return HttpResponse{Status: "error", Info: fmt.Sprintf("No captures of %s for year %s", URL, year)}
	}

	captures := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(captures) > 0 {
		return HttpResponse{Status: "succes", Info: captures}
	}

	return HttpResponse{Status: "error", Info: fmt.Sprintf("No captures of %s for year %s", URL, year)}
}
