package waybackdiscoverdiff

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	s "github.com/mfonda/simhash"
	"golang.org/x/net/html"
)

// TODO: Discover Class
// DONE: ExtractHTMLFeatures

// Process HTML document and get key features as text. Steps:
// kill all script and style elements
// get lowercase text
// remove all punctuation
// break into lines and remove leading and trailing space on each
// break multi-headlines into a line each
// drop blank lines
// return a dict with features and their weights

func ExtractHTMLFeatures(htmlContent string) map[string]int {
	// fmt.Println(htmlContent)
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	// return a dict with features and their weights
	features := make(map[string]int)
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
			lowercaseData := strings.Fields(strings.ToLower(n.Data))

			var builder strings.Builder
			for _, str := range lowercaseData {
				for i, r := range str {
					// remove punctuation
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
				fmt.Println(builder.String())
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
	Hash      uint64
	BitLength int
}

func CalculateSimhash(features map[string]int, bitLength int) (simhash Simhash) {
	stringFeatures := fmt.Sprint(features)
	byteFeatures := []byte(stringFeatures)
	hash := s.Simhash(s.NewWordFeatureSet(byteFeatures))
	simhash.Hash = hash
	simhash.BitLength = bitLength
	return
}

func PackSimhashToBytes(simhash Simhash, bitLength int) int {
	// return bits.Len64(simhash.Hash)
	return bitLength / 8
}
