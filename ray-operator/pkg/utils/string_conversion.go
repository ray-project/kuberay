// Util functions for type conversion between byte array and string without copy.
//
// Example usage:
// func TakeByteSlice(bs []byte) {...}
//
// func f() {
//   s := "helloworld"
//   TakeByteSlice(ConvertStringToByteArray(s))  // convert string to byte slice with zero-copy
// }

package utils

import (
	"unsafe"
)

// Convert a byte array to string w/o copy.
//
// WARNING: The returned byte slice is not expected to change.
func ConvertByteArrayToString(arr []byte) string {
	return unsafe.String(&arr[0], len(arr))
}

// Convert a string to byte array w/o copy.
//
// WARNING: The returned byte slice is not expected to change.
func ConvertStringToByteArray(s string) (arr []byte) {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
