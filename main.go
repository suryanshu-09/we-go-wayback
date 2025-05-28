package main

import (
	"fmt"

	d "github.com/suryanshu-09/we-go-wayback/waybackdiscoverdiff"
)

// func main() {
// 	fmt.Println("Hello from We-Go-Wayback!")
// }

// var ctx = context.Background()
//
// func main() {
// 	rdb := redis.NewClient(&redis.Options{
// 		Addr:     "localhost:6379",
// 		Password: "", // no password set
// 		DB:       0,  // use default DB
// 	})
//
// 	err := rdb.Set(ctx, "key", "value", 0).Err()
// 	if err != nil {
// 		panic(err)
// 	}
//
// 	val, err := rdb.Get(ctx, "key").Result()
// 	if err != nil {
// 		panic(err)
// 	}
// 	fmt.Println("key", val)
//
// 	val2, err := rdb.Get(ctx, "key2").Result()
// 	if err == redis.Nil {
// 		fmt.Println("key2 does not exist")
// 	} else if err != nil {
// 		panic(err)
// 	} else {
// 		fmt.Println("key2", val2)
// 	}
// 	// Output: key value
// 	// key2 does not exist
// }

func main() {
	html := `<Html>
    <something>weird is happening \c\x0b
    <span>tag</span><span>tag</span>
    </HTML>`

	features := map[string]int{"c": 1, "weird": 1, "is": 1, "happening": 1, "tag": 2}
	got := d.ExtractHTMLFeatures(html)
	fmt.Println(got)
	fmt.Println(features)
	// escaped := `Invalid /\x94Invalid\x0b`
	// parsed, err := strconv.Unquote(`"` + escaped + `"`)
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println(parsed)
}
