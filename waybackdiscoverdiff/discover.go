package waybackdiscoverdiff

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// TODO: fix types
// TODO: implementation details
type SimhashConfig struct {
	Size        int
	ExpireAfter int
}

type RedisConfig struct {
	URL             string
	DecodeResponses bool
	Timeout         int
}

type SnapshotsConfig struct {
	NumberPerYear int
	NumberPerPage int
}

type CFG struct {
	Simhash   SimhashConfig
	Redis     RedisConfig
	Threads   int
	Snapshots SnapshotsConfig
}

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
	var featureData []string
	for n := range doc.Descendants() {
		switch n.Type {
		case html.ElementNode:
			// kill all script and style elements
			if n.Data == "script" || n.Data == "style" {
				// fmt.Println("script or style tags")
				continue
			}
		case html.CommentNode:
			// fmt.Println("CommentNode")
			continue
		case html.TextNode:
			// get lowercase text
			lowercaseData := strings.ToLower(n.Data)
			var builder strings.Builder
			for _, r := range lowercaseData {
				// remove punctuation
				if unicode.IsPunct(r) {
					if r == '\\' {
						builder.WriteRune(r)
						continue
					}
					builder.WriteRune(' ')
					continue
				}
				builder.WriteRune(r)
			}
			cleanData := builder.String()
			featureData = append(featureData, strings.Fields(cleanData)...)
		}
	}

	// return a dict with features and their weights
	features := make(map[string]int)

	for _, feat := range featureData {
		features[feat]++
	}

	return features
}

func CalculateSimhash(features map[string]int, bitLength int) (simhash string) {
	return
}

func Discover(cfg CFG) (__something__ interface{}) {
	return
}

func PackSimhashToBytes(simhash string, bitLength int) (__something__ interface{}) {
	return
}
