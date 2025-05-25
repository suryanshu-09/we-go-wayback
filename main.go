package main

import (
	"fmt"

	d "github.com/suryanshu-09/we-go-wayback/waybackdiscoverdiff"
)

// func main() {
// 	fmt.Println("Hello from We-Go-Wayback!")
// }

func main() {
	html := `<html>
<title>Invalid /\x94Invalid\x0b"</title>
<body>
今日は

</body>
</html>`
	features := d.ExtractHTMLFeatures(html)
	fmt.Println("features: ", features)
	want := map[string]int{"\x94invalid": 1, "invalid": 1, "今日は": 1}
	fmt.Println("want: ", want)
}
