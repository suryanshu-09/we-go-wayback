package waybackdiscoverdiff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"regexp"
	"strings"

	"github.com/crossedbot/simplesurt"
	"github.com/go-redis/redis/v8"
)

// """URL validation.
// """
var EmailRe = regexp.MustCompile(`^[a-zA-Z0-9_.+\-]+@[a-zA-Z0-9\-]+\.[a-zA-Z0-9\-.]+$`)

func UrlIsValid(rawUrl *string) bool {
	if *rawUrl == "" || EmailRe.MatchString(*rawUrl) {
		return false
	}

	// Add scheme if missing
	if !strings.HasPrefix(*rawUrl, "http://") && !strings.HasPrefix(*rawUrl, "https://") {
		*rawUrl = "http://" + *rawUrl
	}

	parsed, err := url.ParseRequestURI(*rawUrl)
	if err != nil || parsed.Hostname() == "" {
		return false
	}

	parts := strings.Split(parsed.Hostname(), ".")
	return len(parts) >= 2 && parts[len(parts)-1] != "" && parts[len(parts)-2] != ""
}

// """Get stored simhash data for url, year and page (optional).
// """

var (
	ErrNoCaptures  = errors.New("no captures")
	ErrNotCaptured = errors.New("no captures")
)

func YearSimhash(redis *redis.Client, url string, year string, opt ...int) ([][2]string, int, error) {
	page := 0
	snapshotsPerPage := 0
	if len(opt) > 0 {
		page = opt[0]
	}
	if len(opt) > 1 {
		snapshotsPerPage = opt[1]
	}

	if url == "" || year == "" {
		return nil, 0, ErrNotCaptured
	}

	ctx := context.Background()
	simple, _ := simplesurt.Format(url)

	re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
	keyUrl := re.ReplaceAllString(simple, "$1)/")
	results, err := redis.HKeys(ctx, keyUrl).Result()
	if err != nil {
		slog.Error("error loading simhash data", "url", url, "year", year, "page", page, "err", err)
		return nil, 0, ErrNotCaptured
	}

	var timestampsToFetch []string
	for _, timestamp := range results {
		if timestamp == year {
			return nil, 0, ErrNoCaptures
		}
		if strings.HasPrefix(timestamp, year) {
			timestampsToFetch = append(timestampsToFetch, timestamp)
		}
	}

	if len(timestampsToFetch) > 0 {
		return handleResults(ctx, redis, timestampsToFetch, url, snapshotsPerPage, page)
	}

	return nil, 0, ErrNotCaptured
}

// """Utility method used by `year_simhash`
// """
func handleResults(ctx context.Context, redis *redis.Client, timestampsToFetch []string, url string, snapshotsPerPage int, page int) ([][2]string, int, error) {
	numberOfPages := int(math.Ceil(float64(len(timestampsToFetch)) / float64(snapshotsPerPage)))

	if page > 0 {
		page = min(page, numberOfPages)
		if numberOfPages > 0 {
			start := (page - 1) * snapshotsPerPage
			end := min(page*snapshotsPerPage, len(timestampsToFetch))
			timestampsToFetch = timestampsToFetch[start:end]
		}
	}

	simple, _ := simplesurt.Format(url)

	re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
	keyUrl := re.ReplaceAllString(simple, "$1)/")
	results, err := redis.HMGet(ctx, keyUrl, timestampsToFetch...).Result()
	if err != nil {
		slog.Error("cannot handle results", "url", url, "page", page, "error", err.Error())
		return nil, 0, err
	}

	var availableSimhashes [][2]string
	for i, val := range results {
		if val == nil {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		availableSimhashes = append(availableSimhashes, [2]string{timestampsToFetch[i], strVal})
	}

	if page > 0 {
		availableSimhashes = append([][2]string{{"pages", fmt.Sprint(numberOfPages)}}, availableSimhashes...)
	}

	return availableSimhashes, len(timestampsToFetch), nil
}

// """Get stored simhash data from Redis for URL and timestamp
// """
func GetTimestampSimhash(redis *redis.Client, url, timestamp string) HttpResponse {
	if url != "" && timestamp != "" {
		ctx := context.Background()
		simple, _ := simplesurt.Format(url)

		re := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://\(([^)]+),\)$`)
		keyUrl := re.ReplaceAllString(simple, "$1)/")

		results, err := redis.HGet(ctx, keyUrl, timestamp).Result()
		if err == nil && results != "-1" {
			return HttpResponse{Status: "success", Simhash: results}
		}

		results, err = redis.HGet(ctx, keyUrl, timestamp[:4]).Result()
		if err == nil && results == "-1" {
			return HttpResponse{Status: "error", Message: "NO_CAPTURES"}
		}

		slog.Error("error loading simhash data", "url", url, "timestamp", timestamp, "error", err)
	}
	return HttpResponse{Status: "error", Message: "CAPTURE_NOT_FOUND"}
}

// """Input: [["20130603143716","NRyJrLc2FWA="],["20130402202841","FT6d7Jc3vWA="],...]
// Output:
// Captures: [[2013, [06, [03, ['143716', 0]]],
//
//	        [04, [02, ['202841', 1]]]
//	]]
//
// Hashes: ['NRyJrLc2FWA=', 'FT6d7Jc3vWA=']
// """
func CompressCaptures(captures [][2]string) ([][]any, []string) {
	hashDict := make(map[string]int)
	grouped := make(map[string]map[string]map[string][][]any)

	for _, pair := range captures {
		ts := pair[0]
		simhash := pair[1]

		year, month, day, hms := ts[0:4], ts[4:6], ts[6:8], ts[8:]

		hashID, exists := hashDict[simhash]
		if !exists {
			hashID = len(hashDict)
			hashDict[simhash] = hashID
		}

		if _, ok := grouped[year]; !ok {
			grouped[year] = make(map[string]map[string][][]any)
		}
		if _, ok := grouped[year][month]; !ok {
			grouped[year][month] = make(map[string][][]any)
		}
		grouped[year][month][day] = append(grouped[year][month][day], []any{hms, hashID})
	}

	newCaptures := [][]any{}
	for y, months := range grouped {
		yearEntry := []any{y}
		for m, days := range months {
			monthEntry := []any{m}
			for d, caps := range days {
				dayEntry := []any{d}
				for _, cap := range caps {
					dayEntry = append(dayEntry, cap)
				}
				monthEntry = append(monthEntry, dayEntry)
			}
			yearEntry = append(yearEntry, monthEntry)
		}
		newCaptures = append(newCaptures, yearEntry)
	}

	hashes := make([]string, len(hashDict))
	for hash, id := range hashDict {
		hashes[id] = hash
	}

	return newCaptures, hashes
}
