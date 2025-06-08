package tests

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/smira/go-statsd"
	s "github.com/suryanshu-09/simhash"
	d "github.com/suryanshu-09/we-go-wayback/waybackdiscoverdiff"
)

func TestExtractHTMLFeatures(t *testing.T) {
	t.Run("handle html with repeated elements and spaces", func(t *testing.T) {
		html := `<html>
<title>my title</title>
<body>
abc
test

123
abc
  space
</body>
</html>`

		features := map[string]int{"123": 1, "abc": 2, "my": 1, "test": 1, "title": 1, "space": 1}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})

	t.Run("handle html with repeated elements, and punctuation", func(t *testing.T) {
		html := `<html>
<title>my title</title>
<body>
abc
a.b.c.
abc.
test
123
abc
</body>
</html>`
		features := map[string]int{"123": 1, "a": 1, "abc": 3, "b": 1, "c": 1, "my": 1, "test": 1, "title": 1}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})

	t.Run("handle plain text", func(t *testing.T) {
		html := "just a string"

		features := map[string]int{"just": 1, "a": 1, "string": 1}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})

	t.Run("skip HTML comments", func(t *testing.T) {
		html := `<html><head>
</head><body>
<!--[if lt IE 9]>
<!-- Important Owl stylesheet -->
<link rel="stylesheet" href="css/owl.carousel.css">
<!-- Default Theme -->
<link rel="stylesheet" href="css/owl.theme.css">
<script src="js/html5shiv.js"></script>
<script src="js/respond.min.js"></script>
<![endif]-->
<p>Thank you for closing the message box.</p>
<a href="/subpage">test</a>
</body></html>`

		features := map[string]int{"box": 1, "closing": 1, "for": 1, "message": 1, "test": 1, "thank": 1, "the": 1, "you": 1}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})

	t.Run("it doesn't crash with invalid or unicode chars", func(t *testing.T) {
		html := `<html>
<title>Invalid /\x94Invalid\x0b"</title>
<body>
今日は

</body>
</html>`

		features := map[string]int{"\x94invalid": 1, "invalid": 1, "今日は": 1}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})

	t.Run("something weird is happening??", func(t *testing.T) {
		html := `<Html>
    <something>weird is happening \c\x0b
    <span>tag</span><span>tag</span>
    </HTML>`

		features := map[string]int{"c": 1, "weird": 1, "is": 1, "happening": 1, "tag": 2}

		got := d.ExtractHTMLFeatures(html)

		assertExtractHTMLFeatures(t, got, features)
	})
}

func assertExtractHTMLFeatures(t testing.TB, got map[string]int, want map[string]int) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got: %v\nwant: %v", got, want)
	}
}

func BenchmarkExtractHTMLFeatures(b *testing.B) {
	html := `<html>
<title>my title</title>
<body>
abc
a.b.c.
abc.
test
123
abc
</body>
</html>`
	for b.Loop() {
		d.ExtractHTMLFeatures(html)
	}
}

func TestCalculateSimhash(t *testing.T) {
	t.Run("Testing Calculate Simhash", func(t *testing.T) {
		features := map[string]int{"two": 2, "three": 3, "one": 1}

		simhash := d.CalculateSimhash(features, 128)
		got := fmt.Sprint(simhash.Hash)

		want := "66237222457941138286276456718971054176"

		assertCalculateSimhash(t, got, want)
	})
}

func assertCalculateSimhash(t testing.TB, got, want string) {
	t.Helper()
	if strings.Compare(got, want) != 0 {
		t.Errorf("got: %s\nwant: %s", got, want)
	}
}

func TestHash(t *testing.T) {
	t.Run("test regular hash", func(t *testing.T) {
		features := map[string]int{
			"2019":             1,
			"advanced":         1,
			"google":           1,
			"google©":          1,
			"history":          1,
			"insearch":         1,
			"more":             1,
			"optionssign":      1,
			"privacy":          1,
			"programsbusiness": 1,
			"searchimagesmapsplayyoutubenewsgmaildrivemorecalendartranslatemobilebooksshoppingbloggerfinancephotosvideosdocseven": 1,
			"searchlanguage":   1,
			"settingsweb":      1,
			"solutionsabout":   1,
			"terms":            1,
			"toolsadvertising": 1,
			"»account":         1,
		}

		hSize := 128

		assertHash(t, features, hSize)
	})

	t.Run("test shortened hash", func(t *testing.T) {
		features := map[string]int{
			"about": 1,
			"accountsearchmapsyoutubeplaynewsgmailcontactsdrivecalendartranslatephotosshoppingmorefinancedocsbooksbloggerhangoutskeepjamboardearthcollectionseven": 1,
			"at":                          1,
			"data":                        1,
			"feedbackadvertisingbusiness": 1,
			"from":                        1,
			"gmailimagessign":             1,
			"google":                      3,
			"helpsend":                    1,
			"in":                          2,
			"inappropriate":               1,
			"library":                     1,
			"local":                       1,
			"more":                        1,
			"new":                         1,
			"predictions":                 1,
			"privacytermssettingssearch":  1,
			"remove":                      1,
			"report":                      1,
			"searchhistorysearch":         1,
			"searchyour":                  1,
			"settingsadvanced":            1,
			"skills":                      1,
			"store":                       1,
			"with":                        1,
			"your":                        1,
			"×develop":                    1,
		}

		hSize := 128

		assertHash(t, features, hSize)
	})

	t.Run("test simhash 256", func(t *testing.T) {
		features := map[string]int{
			"2019":              1,
			"advanced":          1,
			"at":                1,
			"google":            1,
			"googleadvertising": 1,
			"google©":           1,
			"history":           1,
			"insearch":          1,
			"library":           1,
			"local":             1,
			"more":              1,
			"new":               1,
			"optionssign":       1,
			"privacy":           1,
			"programsbusiness":  1,
			"searchimagesmapsplayyoutubenewsgmaildrivemorecalendartranslatemobilebooksshoppingbloggerfinancephotosvideosdocseven": 1,
			"searchlanguage": 1,
			"settingsweb":    1,
			"skills":         1,
			"solutionsabout": 1,
			"terms":          1,
			"toolsdevelop":   1,
			"with":           1,
			"your":           1,
			"»account":       1,
		}

		hSize := 256

		assertHash(t, features, hSize, d.CustomHashFunc)
	})
}

func assertHash(t testing.TB, features map[string]int, hSize int, hashFuncs ...s.HashFunc) {
	t.Helper()

	var got d.Simhash
	if len(hashFuncs) > 0 && hashFuncs[0] != nil {
		got = d.CalculateSimhash(features, hSize, hashFuncs[0])
		wantBitLength := hSize
		if got.BitLength != wantBitLength {
			t.Errorf("got: %d\nwant: %d", got.BitLength, wantBitLength)
		}

	} else {
		got = d.CalculateSimhash(features, hSize)
		wantBitLength := hSize
		if got.BitLength != wantBitLength {
			t.Errorf("got: %d\nwant: %d", got.BitLength, wantBitLength)
		}

	}
	gotBytes := d.PackSimhashToBytes(&got, hSize)
	wantBytesLen := hSize / 8

	if len(gotBytes) != wantBytesLen {
		t.Errorf("got: %d\nwant: %d", len(gotBytes), wantBytesLen)
	}
}

var cfg = d.CFG{
	Simhash: d.CFGSimhash{
		Size:        256,
		ExpireAfter: 86400,
	},
	Redis: &redis.Options{
		Addr:        "redis://localhost:6379/1",
		DialTimeout: 10 * time.Second,
	},
	Threads: 5,
	Snapshots: d.Snapshots{
		NumberPerYear: -1,
		NumberPerPage: 600,
	},
}

func TestDownloadCapture(t *testing.T) {
	d.STATSDClient = statsd.NewClient("localhost:8125")
	d := d.NewDiscover(cfg)
	d.Url = "https://iskme.org"

	data := d.DownloadCapture("20190103133511")
	if data == nil {
		t.Error("expected capture data, got nil")
	}
}
