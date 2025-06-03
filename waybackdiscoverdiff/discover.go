package waybackdiscoverdiff

import (
	"math/big"
	"strconv"
	"strings"
	"unicode"

	s "github.com/suryanshu-09/simhash/simhash"
	"golang.org/x/crypto/blake2b"
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

func CustomHashFunc(data []byte) []byte {
	hash := blake2b.Sum512(data)
	return hash[:]
}
