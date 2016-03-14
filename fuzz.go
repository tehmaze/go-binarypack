// +build gofuzz

package binarypack

func Fuzz(data []byte) int {
	var i interface{}
	if err := Unmarshal(data, i); err != nil {
		panic(err)
	}
	return 1
}
