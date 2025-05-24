package waybackdiscoverdiff

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

func ExtractHTMLFeatures(html string) (features map[string]int) {
	return
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
