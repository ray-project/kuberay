// Util functions for type conversion between byte array and string.

package utils

import (
	"unsafe"
)

// Convert a byte array to string w/o copy.
func ConvertByteArrayToString(arr []byte) string {
	return unsafe.String(&arr[0], len(arr))
}

// Convert a string to byte array w/o copy.
func ConvertStringToByteArray(s string) (arr []byte) {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
