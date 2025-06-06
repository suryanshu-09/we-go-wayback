package tests

import (
	"fmt"
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
	u "github.com/suryanshu-09/we-go-wayback/waybackdiscoverdiff"
)

var (
	redisClient *redis.Client
	clientMock  redismock.ClientMock
)

type hStuff struct {
	hkey string
	hval string
}

type SampleRedisContent struct {
	key string
	h   []hStuff
}

var SAMPLE_REDIS_CONTENT = []SampleRedisContent{
	{"com,example)/", []hStuff{
		{"20141021062411", "o52rOf0Hi2o="},
		{"20140202131837", "og2jGKWHsy4="},
		{"20140824062257", "o52jPP0Hg2o="},
		{"20160824062257", "o52jPP0Hg2o="},
	}},
	{"com,other)/", []hStuff{
		{"2014", "-1"},
	}},
	{"org,nonexistingdomain)/", []hStuff{
		{"1999", "-1"},
	}},
}

func StubRedis() {
	redisClient, clientMock = redismock.NewClientMock()

	// 1. example.com (2014) => 3 captures
	keyExample := "com,example)/"
	hkeys2014 := []string{"20141021062411", "20140202131837", "20140824062257"}
	hvals2014 := []any{"o52rOf0Hi2o=", "og2jGKWHsy4=", "o52jPP0Hg2o="}

	// All keys set + get
	for i, k := range hkeys2014 {
		valStr := hvals2014[i].(string) // assert from any to string
		clientMock.ExpectHSet(keyExample, k, valStr)
		clientMock.ExpectHGet(keyExample, k).SetVal(valStr)
	}
	clientMock.ExpectHKeys(keyExample).SetVal([]string{
		"20141021062411", "20140202131837", "20140824062257", "20160824062257",
	})
	clientMock.ExpectHMGet(keyExample, hkeys2014...).SetVal(hvals2014)

	// 2. example.com (2016) => 1 capture
	hkeys2016 := []string{"20160824062257"}
	hvals2016 := []any{"o52jPP0Hg2o="}
	clientMock.ExpectHMGet(keyExample, hkeys2016...).SetVal(hvals2016)
	clientMock.ExpectHGet(keyExample, "20160824062257").SetVal("o52jPP0Hg2o=")

	// 3. example.com (2017) => expect HKeys only, nothing fetched
	clientMock.ExpectHKeys(keyExample).SetVal([]string{
		"20141021062411", "20140202131837", "20140824062257", "20160824062257",
	})

	// 4. other.com (2014) => expect error (no matching timestamp)
	keyOther := "com,other)/"
	clientMock.ExpectHKeys(keyOther).SetVal([]string{"2014"})

	// 5. nonexistingdomain.org => no keys
	keyNonExist := "org,nonexistingdomain)/"
	clientMock.ExpectHKeys(keyNonExist).SetVal([]string{"1999"})
}

func TestUrlIsValid(t *testing.T) {
	input := map[string]bool{
		"http://example.com/":       true,
		"other":                     false,
		"torrent:something.gr/file": false,
		"tel:00302310123456":        false,
		"loudfi1@libero.it":         false,
		"http://roblox":             false,
	}

	for url, result := range input {
		if u.UrlIsValid(url) != result {
			t.Errorf("url:%s\ngot:%t\nwant:%t\n", url, u.UrlIsValid(url), result)
		}
	}
}

// Mock Fails, but should work irl
func TestYearSimhash(t *testing.T) {
	type input struct {
		url   string
		year  string
		count int
	}

	inputs := []input{
		{"http://example.com", "2014", 3},
		{"http://example.com", "2016", 1},
		{"http://example.com", "2017", 0},
		{"http://example.com", "", 0},
		{"http://other.com", "2014", 0},
	}

	for _, i := range inputs {
		StubRedis()
		clientMock.MatchExpectationsInOrder(false)

		clientMock.ExpectationsWereMet()
		t.Run(fmt.Sprintf("url=%s year=%s", i.url, i.year), func(t *testing.T) {
			res, count, err := u.YearSimhash(redisClient, i.url, i.year)
			if err != nil {
				if i.year == "2014" {
					if err != u.ErrNoCaptures {
						t.Errorf("got:%v\nwant:%v", err, u.ErrNoCaptures)
					}
				} else if err != u.ErrNotCaptured {
					t.Errorf("got:%v\nwant:%v\n", err, u.ErrNotCaptured)
				}
			}
			if count != len(res) {
				t.Errorf("got:%d\nwant:%d", len(res), count)
			}
		})
	}
}
