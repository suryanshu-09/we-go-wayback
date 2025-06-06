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
	"github.com/redis/go-redis/v9"
)

// """URL validation.
// """
var EmailRe = regexp.MustCompile(`^[a-zA-Z0-9_.+\-]+@[a-zA-Z0-9\-]+\.[a-zA-Z0-9\-.]+$`)

func UrlIsValid(rawUrl string) bool {
	if rawUrl == "" {
		return false
	}
	if EmailRe.MatchString(rawUrl) {
		return false
	}

	parsed, err := url.Parse(rawUrl)
	if err != nil || parsed.Host == "" {
		return false
	}

	hostParts := strings.Split(parsed.Hostname(), ".")
	return len(hostParts) >= 2 && hostParts[len(hostParts)-1] != "" && hostParts[len(hostParts)-2] != ""
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
